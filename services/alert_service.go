package services

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/everestp/deping-client-service/db/repositories"
	"github.com/everestp/deping-client-service/dto"
)

type MessageSender interface {
	SendMessage(ctx context.Context, chatID int64, text string) error
}

type AlertService interface {
	ProcessPingResult(ctx context.Context, result dto.PingResultItem) error
	SendDirectMessage(ctx context.Context, chatID int64, userID int, text string) error
}

type alertService struct {
	repo   repositories.TelegramRepository
	sender MessageSender
	log    *slog.Logger
}

func NewAlertService(repo repositories.TelegramRepository, sender MessageSender, log *slog.Logger) AlertService {
	return &alertService{repo: repo, sender: sender, log: log}
}

func (a *alertService) ProcessPingResult(ctx context.Context, result dto.PingResultItem) error {
	if result.MonitorID == "" {
		return nil
	}

	state, err := a.repo.GetHealthState(ctx, result.MonitorID)
	if err != nil {
		return fmt.Errorf("state lookup: %w", err)
	}

	// DIAGNOSTIC LOG: Watch this in your console to see the two-packet logic in action
	a.log.Debug("processing ping",
		"monitor_id", result.MonitorID,
		"success", result.Success,
		"consecutive_fails", state.ConsecutiveFailures,
		"is_down", state.IsDown,
	)

	var shouldAlert bool
	var alertEvent *dto.AlertEvent

	if !result.Success {
		state.ConsecutiveFailures++
		if state.ConsecutiveFailures >= 2 && !state.IsDown {
			state.IsDown = true
			state.AlertSent = true
			state.LastAlertedAt = sql.NullTime{Time: time.Now(), Valid: true}
			shouldAlert = true
			alertEvent = buildDownAlert(result)
			a.log.Info("ALERT: Monitor transitioned to DOWN", "monitor_id", result.MonitorID)
		}
	} else {
		if state.IsDown && state.AlertSent {
			shouldAlert = true
			alertEvent = buildRestoredAlert(result)
			a.log.Info("ALERT: Monitor transitioned to RESTORED", "monitor_id", result.MonitorID)
		}
		state.ConsecutiveFailures = 0
		state.IsDown = false
		state.AlertSent = false
	}

	if err := a.repo.UpsertHealthState(ctx, state); err != nil {
		a.log.Error("failed to persist state", "monitor_id", result.MonitorID, "err", err)
	}

	if shouldAlert && alertEvent != nil {
		if err := a.dispatchAlert(ctx, alertEvent); err != nil {
			a.log.Error("dispatch alert failure", "monitor_id", result.MonitorID, "err", err)
		}
	}

	return nil
}

func (a *alertService) dispatchAlert(ctx context.Context, event *dto.AlertEvent) error {
	enabled, err := a.repo.IsNotificationEnabled(ctx, event.MonitorID)
	if err != nil || !enabled {
		return err
	}

	chatID, userID, err := a.repo.GetMonitorOwnerChatID(ctx, event.MonitorID)
	if err != nil || chatID == 0 {
		return err
	}

	ok, _, err := a.repo.DeductCredit(ctx, userID)
	if err != nil {
		return err
	}
	if !ok {
		return a.sendCreditReminder(ctx, chatID, userID)
	}

	return a.sender.SendMessage(ctx, chatID, formatAlertMessage(event))
}

func (a *alertService) SendDirectMessage(ctx context.Context, chatID int64, userID int, text string) error {
	return a.sender.SendMessage(ctx, chatID, text)
}

func (a *alertService) sendCreditReminder(ctx context.Context, chatID int64, userID int) error {
	should, err := a.repo.ShouldSendReminder(ctx, userID)
	if err != nil || !should {
		return err
	}

	const msg = `⚠️ *Notification Paused — Credits Exhausted*

You've used all 3 free daily messages and have no purchased credits left.

Alerts for your monitors are being suppressed until you top up.

👉 Go to your dashboard → *Telegram* section to purchase credits using SOL tokens.

_You'll receive this reminder once a week until credits are added._`

	if err := a.sender.SendMessage(ctx, chatID, msg); err != nil {
		return err
	}
	return a.repo.MarkReminderSent(ctx, userID)
}

func buildDownAlert(r dto.PingResultItem) *dto.AlertEvent {
	return &dto.AlertEvent{
		MonitorID: r.MonitorID, TargetURL: r.TargetURL, IsDown: true, LatencyMs: r.LatencyMs,
		StatusCode: r.StatusCode, ErrorKind: r.ErrorKind, GeoRegion: r.GeoRegion,
		Timestamp: time.UnixMilli(r.TimestampMs).UTC(),
	}
}

func buildRestoredAlert(r dto.PingResultItem) *dto.AlertEvent {
	return &dto.AlertEvent{
		MonitorID: r.MonitorID, TargetURL: r.TargetURL, IsDown: false, LatencyMs: r.LatencyMs,
		StatusCode: r.StatusCode, Timestamp: time.UnixMilli(r.TimestampMs).UTC(),
	}
}

func formatAlertMessage(e *dto.AlertEvent) string {
	ts := e.Timestamp.Format("2006-01-02 15:04:05 UTC")
	if e.IsDown {
		errorLine := ""
		if e.ErrorKind != "" { errorLine = fmt.Sprintf("\n🔖 *Error:* `%s`", e.ErrorKind) }
		statusLine := ""
		if e.StatusCode > 0 { statusLine = fmt.Sprintf("\n📋 *Status:* `%d`", e.StatusCode) }
		return fmt.Sprintf("🔴 *SERVICE DOWN*\n\n🌐 *URL:* %s\n🌍 *Region:* %s%s%s\n🕐 *Detected:* %s\n\nYour monitor has failed 2 consecutive checks.",
			e.TargetURL, e.GeoRegion, statusLine, errorLine, ts)
	}
	return fmt.Sprintf("✅ *SERVICE RESTORED*\n\n🌐 *URL:* %s\n⚡ *Latency:* %dms\n🕐 *Recovered:* %s\n\nYour monitor is back online.",
		e.TargetURL, e.LatencyMs, ts)
}
