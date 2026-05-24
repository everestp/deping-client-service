package bot

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	tele "gopkg.in/telebot.v3"
	"gopkg.in/telebot.v3/middleware"

	"github.com/everestp/deping-client-service/db/repositories"
	"github.com/everestp/deping-client-service/services"
)

type Bot struct {
	tele            *tele.Bot
	telegramService services.TelegramService
	alertService    services.AlertService
	telegramRepo    repositories.TelegramRepository
	log             *slog.Logger
}

func NewBot(token string, telegramService services.TelegramService, alertService services.AlertService, telegramRepo repositories.TelegramRepository, log *slog.Logger) (*Bot, error) {
	pref := tele.Settings{
		Token:  token,
		Poller: &tele.LongPoller{Timeout: 10 * time.Second},
	}
	b, err := tele.NewBot(pref)
	if err != nil {
		return nil, fmt.Errorf("create telebot: %w", err)
	}

	return &Bot{
		tele:            b,
		telegramService: telegramService,
		alertService:    alertService,
		telegramRepo:    telegramRepo,
		log:             log,
	}, nil
}

func (b *Bot) SetAlertService(svc services.AlertService) {
	b.alertService = svc
	b.registerHandlers()
}

func (b *Bot) Start() { b.tele.Start() }
func (b *Bot) Stop()  { b.tele.Stop() }

func (b *Bot) registerHandlers() {
	b.tele.Use(middleware.Recover())

	b.tele.Handle("/start", b.handleStart)
	b.tele.Handle("/help", b.handleHelp)
	b.tele.Handle("/verify", b.handleVerify)

	b.tele.Handle("/monitor", b.withAuth(b.handleMonitor))
	b.tele.Handle("/credits", b.withAuth(b.handleCredits))
	b.tele.Handle("/status", b.withAuth(b.handleStatus))

	b.tele.Handle(&tele.Btn{Unique: "show_monitor"}, b.withAuth(b.handleShowMonitor))
	b.tele.Handle(&tele.Btn{Unique: "toggle_notify"}, b.withAuth(b.handleToggleNotify))
}

func (b *Bot) withAuth(next tele.HandlerFunc) tele.HandlerFunc {
	return func(c tele.Context) error {
		// Use Background context for the DB check
		ctx := context.Background()
		tu, err := b.telegramRepo.GetTelegramUserByChatID(ctx, c.Sender().ID)
		if err != nil {
			b.log.Error("Auth DB Error", "err", err, "chatID", c.Sender().ID)
			return c.Send("⚠️ System busy. Please try again.")
		}
		if tu == nil || !tu.IsVerified {
			return c.Send("🚫 *Access Denied*\nPlease link your account at [deping.xyz/settings](https://deping.xyz/settings)", tele.ModeMarkdown)
		}
		return next(c)
	}
}

// ── Handlers ─────────────────────────────────────────────────────────────

func (b *Bot) handleStart(c tele.Context) error {
	ctx := context.Background()
	tu, _ := b.telegramRepo.GetTelegramUserByUsername(ctx, c.Sender().Username)
	if tu == nil {
		return c.Send("👋 *Welcome to DePIN Monitor*\n\nPlease link your Telegram via [deping.xyz](https://deping.xyz/settings) to begin.", tele.ModeMarkdown)
	}

	_ = b.telegramRepo.UpdateChatID(ctx, tu.UserID, c.Sender().ID)

	if tu.IsVerified {
		return c.Send("✅ *Account Linked!*\nUse /help to see commands.", tele.ModeMarkdown)
	}
	return c.Send("⚠️ *Pending Verification*\nVerify using: `/verify <code>`", tele.ModeMarkdown)
}

func (b *Bot) handleHelp(c tele.Context) error {
	return c.Send("🤖 *DePIN Assistant Commands*\n\n"+
		"📡 /monitor - View active infrastructure\n"+
		"📊 /status - System summary\n"+
		"💰 /credits - Check balance", tele.ModeMarkdown)
}

func (b *Bot) handleVerify(c tele.Context) error {
	args := c.Args()
	if len(args) == 0 {
		return c.Send("Usage: `/verify <code>`", tele.ModeMarkdown)
	}
	_, err := b.telegramService.VerifyLink(context.Background(), strings.TrimSpace(args[0]), c.Sender().ID, c.Sender().Username)
	if err != nil {
		return c.Send("❌ *Verification failed.* Please check your code.", tele.ModeMarkdown)
	}
	return c.Send("✨ *Account successfully linked!*", tele.ModeMarkdown)
}

func (b *Bot) handleMonitor(c tele.Context) error {
	summaries, _ := b.telegramRepo.GetUserMonitorSummaries(context.Background(), c.Sender().ID)
	if len(summaries) == 0 {
		return c.Send("🔍 *No monitors found.* Add them at [deping.xyz](https://deping.xyz).", tele.ModeMarkdown)
	}

	menu := &tele.ReplyMarkup{}
	var rows []tele.Row
	for _, s := range summaries {
		btn := menu.Data(fmt.Sprintf("📡 %s", s.TargetURL), "show_monitor", s.ID)
		rows = append(rows, menu.Row(btn))
	}
	menu.Inline(rows...)
	return c.Send("📋 *Select a monitor to inspect:*", menu)
}

func (b *Bot) handleShowMonitor(c tele.Context) error {
	monitorID := c.Callback().Data
	pings, _ := b.telegramRepo.GetRecentPings(context.Background(), monitorID, 5)

	var sb strings.Builder
	sb.WriteString("🛰 *Monitor Health*\n")
	for i := len(pings) - 1; i >= 0; i-- {
		if pings[i].Success { sb.WriteString("🟢") } else { sb.WriteString("🔴") }
	}
	sb.WriteString("\n\n`STAT | REG | LATE | TIME`\n`-------------------------`\n")

	for _, p := range pings {
		status, lat := "OK  ", fmt.Sprintf("%dms", p.LatencyMs)
		if !p.Success {
			status, lat = "DOWN", fmt.Sprintf("E:%d", p.StatusCode)
		}
		reg := "N/A"
		if len(p.GeoRegion) > 0 { reg = p.GeoRegion[:4] }
		sb.WriteString(fmt.Sprintf("`%s | %-3s | %-4s | %s`\n", status, reg, lat, p.Timestamp.Format("15:04")))
	}

	menu := &tele.ReplyMarkup{}
	menu.Inline(menu.Row(menu.Data("🔔 Toggle Notifications", "toggle_notify", monitorID)))
	return c.Edit(sb.String(), menu, tele.ModeMarkdown)
}

func (b *Bot) handleToggleNotify(c tele.Context) error {
	ctx := context.Background()
	tu, _ := b.telegramRepo.GetTelegramUserByChatID(ctx, c.Sender().ID)
	current, _ := b.telegramService.GetNotificationStatus(ctx, tu.UserID, c.Callback().Data)

	err := b.telegramService.ToggleNotification(ctx, tu.UserID, c.Callback().Data, !current)
	if err != nil {
		return c.Respond(&tele.CallbackResponse{Text: "❌ Update Failed"})
	}
	text := "Notifications Disabled 🔕"
	if !current { text = "Notifications Enabled 🔔" }
	return c.Respond(&tele.CallbackResponse{Text: text})
}

func (b *Bot) handleCredits(c tele.Context) error {
	ctx := context.Background()
	tu, _ := b.telegramRepo.GetTelegramUserByChatID(ctx, c.Sender().ID)
	status, err := b.telegramService.GetCreditStatus(ctx, tu.UserID)
	if err != nil {
		return c.Send("⚠️ *Could not retrieve credit data.*")
	}

	msg := fmt.Sprintf("💰 *Credit Summary*\n\nAvailable: `%d`\nFree Used: `%d`\nNext Reset: `%s`",
		status.TotalCreditsLeft, status.FreeCreditsUsed, status.FreeResetDate)
	return c.Send(msg, tele.ModeMarkdown)
}

func (b *Bot) handleStatus(c tele.Context) error {
	summaries, _ := b.telegramRepo.GetUserMonitorSummaries(context.Background(), c.Sender().ID)
	return c.Send(fmt.Sprintf("📊 *System Overview*\nTotal Active Monitors: `%d`", len(summaries)), tele.ModeMarkdown)
}

func (b *Bot) SendMessage(ctx context.Context, chatID int64, text string) error {
	_, err := b.tele.Send(&tele.Chat{ID: chatID}, text, tele.ModeMarkdown)
	return err
}
