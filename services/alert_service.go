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

// ═══════════════════════════════════════════════════════════════════════════
// AlertService Interface
// ═══════════════════════════════════════════════════════════════════════════

// MessageSender is an abstraction over the Telegram bot sender.
// Implemented by the bot package to avoid circular imports.
type MessageSender interface {
	SendMessage(ctx context.Context, chatID int64, text string) error
}

// AlertService processes ping results, manages the Two-Packet state machine,
// and dispatches Telegram notifications with credit gating.
type AlertService interface {
	// ProcessPingResult applies the Two-Packet rule and fires notifications if warranted.
	ProcessPingResult(ctx context.Context, result dto.PingResultItem) error

	// SendDirectMessage sends a message to a user (e.g. verification code), deducting credits.
	SendDirectMessage(ctx context.Context, chatID int64, userID int, text string) error
}

// ═══════════════════════════════════════════════════════════════════════════
// Thresholds — configurable at startup
// ═══════════════════════════════════════════════════════════════════════════

const (
	// HighLatencyThresholdMs — above this we consider a "slow" warning
	HighLatencyThresholdMs = 3000
	// TwoPacketThreshold — consecutive failures before DOWN alert fires
	TwoPacketThreshold = 2
)

// ═══════════════════════════════════════════════════════════════════════════
// Implementation
// ═══════════════════════════════════════════════════════════════════════════

type alertService struct {
	repo   repositories.TelegramRepository
	sender MessageSender
	log    *slog.Logger
}

func NewAlertService(
	repo repositories.TelegramRepository,
	sender MessageSender,
	log *slog.Logger,
) AlertService {
	return &alertService{repo: repo, sender: sender, log: log}
}

// ProcessPingResult is the heart of the notification pipeline.
//
// State machine (per monitor_id):
//
//	HEALTHY → failure #1 → still HEALTHY (no alert yet)
//	HEALTHY → failure #2 → DOWN      → fire DOWN alert (Two-Packet rule)
//	DOWN    → success    → HEALTHY   → fire RESTORED alert (only if alert_sent=true)
func (a *alertService) ProcessPingResult(ctx context.Context, result dto.PingResultItem) error {
	if result.MonitorID == "" {
		return nil // malformed result; skip silently
	}

	// ── 1. Load current state ────────────────────────────────────────────
	state, err := a.repo.GetHealthState(ctx, result.MonitorID)
	if err != nil {
		return fmt.Errorf("get health state: %w", err)
	}

	// ── 2. Update state machine ──────────────────────────────────────────
	var shouldAlert bool
	var alertEvent *dto.AlertEvent

	if !result.Success {
		// Increment consecutive failures
		state.ConsecutiveFailures++

		if state.ConsecutiveFailures >= TwoPacketThreshold && !state.IsDown {
			// Transition to DOWN — fire alert
			state.IsDown = true
			state.AlertSent = true
			now := time.Now()
			state.LastAlertedAt = sql.NullTime{Time: now, Valid: true}
			shouldAlert = true
			alertEvent = buildDownAlert(result)
		}
	} else {
		// Successful check
		if state.IsDown && state.AlertSent {
			// Transition to HEALTHY — fire RESTORED only if we sent a DOWN alert
			shouldAlert = true
			alertEvent = buildRestoredAlert(result)
		}
		// Reset state
		state.ConsecutiveFailures = 0
		state.IsDown = false
		state.AlertSent = false
	}

	// ── 3. Persist updated state ─────────────────────────────────────────
	if err := a.repo.UpsertHealthState(ctx, state); err != nil {
		a.log.Error("upsert health state failed", "monitor_id", result.MonitorID, "err", err)
	}

	// ── 4. Dispatch notification if warranted ────────────────────────────
	if shouldAlert && alertEvent != nil {
		if err := a.dispatchAlert(ctx, alertEvent); err != nil {
			a.log.Error("dispatch alert failed", "monitor_id", result.MonitorID, "err", err)
		}
	}

	return nil
}

// dispatchAlert checks notification toggle, resolves the owner's Telegram chat,
// gates on credits, and sends the message.
func (a *alertService) dispatchAlert(ctx context.Context, event *dto.AlertEvent) error {
	// ── a. Check notification toggle for this monitor ────────────────────
	enabled, err := a.repo.IsNotificationEnabled(ctx, event.MonitorID)
	if err != nil || !enabled {
		a.log.Debug("notifications disabled", "monitor_id", event.MonitorID)
		return err
	}

	// ── b. Resolve owner's Telegram chat ID ─────────────────────────────
	chatID, userID, err := a.repo.GetMonitorOwnerChatID(ctx, event.MonitorID)
	if err != nil {
		return fmt.Errorf("get owner chat id: %w", err)
	}
	if chatID == 0 {
		// Owner hasn't linked Telegram — skip silently
		return nil
	}

	// ── c. Credit gate ───────────────────────────────────────────────────
	ok, creditType, err := a.repo.DeductCredit(ctx, userID)
	if err != nil {
		return fmt.Errorf("deduct credit: %w", err)
	}
	if !ok {
		// No credits — check if we should send a weekly reminder
		return a.sendCreditReminder(ctx, chatID, userID)
	}

	a.log.Info("sending alert",
		"monitor_id", event.MonitorID,
		"is_down", event.IsDown,
		"credit_type", creditType,
	)

	// ── d. Format and send the message ───────────────────────────────────
	msg := formatAlertMessage(event)
	return a.sender.SendMessage(ctx, chatID, msg)
}

// SendDirectMessage sends a non-alert message to a specific chat (e.g. verification codes).
// This does NOT deduct credits — it's a system message.
func (a *alertService) SendDirectMessage(ctx context.Context, chatID int64, userID int, text string) error {
	return a.sender.SendMessage(ctx, chatID, text)
}

// sendCreditReminder sends a weekly "you're out of credits" message.
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

// ── Alert Message Formatters ──────────────────────────────────────────────

func buildDownAlert(r dto.PingResultItem) *dto.AlertEvent {
	return &dto.AlertEvent{
		MonitorID:  r.MonitorID,
		TargetURL:  r.TargetURL,
		IsDown:     true,
		LatencyMs:  r.LatencyMs,
		StatusCode: r.StatusCode,
		ErrorKind:  r.ErrorKind,
		GeoRegion:  r.GeoRegion,
		Timestamp:  time.UnixMilli(r.TimestampMs).UTC(),
	}
}

func buildRestoredAlert(r dto.PingResultItem) *dto.AlertEvent {
	return &dto.AlertEvent{
		MonitorID:  r.MonitorID,
		TargetURL:  r.TargetURL,
		IsDown:     false,
		LatencyMs:  r.LatencyMs,
		StatusCode: r.StatusCode,
		Timestamp:  time.UnixMilli(r.TimestampMs).UTC(),
	}
}

func formatAlertMessage(e *dto.AlertEvent) string {
	ts := e.Timestamp.Format("2006-01-02 15:04:05 UTC")

	if e.IsDown {
		errorLine := ""
		if e.ErrorKind != "" {
			errorLine = fmt.Sprintf("\n🔖 *Error:* `%s`", e.ErrorKind)
		}
		statusLine := ""
		if e.StatusCode > 0 {
			statusLine = fmt.Sprintf("\n📋 *Status:* `%d`", e.StatusCode)
		}
		return fmt.Sprintf(`🔴 *SERVICE DOWN*

🌐 *URL:* %s
🌍 *Region:* %s%s%s
🕐 *Detected:* %s

Your monitor has failed 2 consecutive checks. We'll notify you when it recovers.`,
			e.TargetURL, e.GeoRegion, statusLine, errorLine, ts)
	}

	return fmt.Sprintf(`✅ *SERVICE RESTORED*

🌐 *URL:* %s
⚡ *Latency:* %dms
🌍 *Region:* %s
🕐 *Recovered:* %s

Your monitor is back online and responding normally.`,
		e.TargetURL, e.LatencyMs, e.GeoRegion, ts)
}
