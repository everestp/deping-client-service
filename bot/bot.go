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

// SetAlertService resolves the circular dependency after initialization
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
		tu, err := b.telegramRepo.GetTelegramUserByChatID(context.Background(), c.Sender().ID)
		if err != nil || tu == nil || !tu.IsVerified {
			return c.Send("Authentication required. Please link your account at https://deping.xyz/settings.")
		}
		return next(c)
	}
}

func (b *Bot) handleStart(c tele.Context) error {
	ctx := context.Background()
	tu, err := b.telegramRepo.GetTelegramUserByUsername(ctx, c.Sender().Username)
	if err != nil || tu == nil {
		return c.Send("Access Denied. Your Telegram account is not linked to any deping.xyz profile.")
	}

	_ = b.telegramRepo.UpdateChatID(ctx, tu.UserID, c.Sender().ID)

	if tu.IsVerified {
		return c.Send("Welcome back. Use /help to see available commands.")
	}
	return c.Send("Authentication required. Please send your code: /verify <code>")
}

func (b *Bot) handleToggleNotify(c tele.Context) error {
	ctx := context.Background()
	monitorID := c.Callback().Data

	tu, _ := b.telegramRepo.GetTelegramUserByChatID(ctx, c.Sender().ID)
	current, _ := b.telegramService.GetNotificationStatus(ctx, tu.UserID, monitorID)

	err := b.telegramService.ToggleNotification(ctx, tu.UserID, monitorID, !current)
	if err != nil {
		return c.Respond(&tele.CallbackResponse{Text: "Failed to update settings."})
	}

	return c.Respond(&tele.CallbackResponse{Text: "Notification settings updated."})
}

// ... (handleHelp, handleVerify, handleMonitor, handleShowMonitor, handleCredits, handleStatus remain as defined previously)

func (b *Bot) handleHelp(c tele.Context) error {
	return c.Send("deping.xyz Monitor\n/monitor - Active status\n/status - System summary\n/credits - Balance")
}

func (b *Bot) handleVerify(c tele.Context) error {
	args := c.Args()
	if len(args) == 0 { return c.Send("Usage: /verify <code>") }
	_, err := b.telegramService.VerifyLink(context.Background(), strings.TrimSpace(args[0]), c.Sender().ID, c.Sender().Username)
	if err != nil { return c.Send("Verification failed.") }
	return c.Send("Linked successfully.")
}

func (b *Bot) handleMonitor(c tele.Context) error {
    summaries, _ := b.telegramRepo.GetUserMonitorSummaries(context.Background(), c.Sender().ID)
    menu := &tele.ReplyMarkup{}
    var rows []tele.Row
    for _, s := range summaries {
        btn := menu.Data(fmt.Sprintf("%s | %s", "MONITOR", s.TargetURL), "show_monitor", s.ID)
        rows = append(rows, menu.Row(btn))
    }
    menu.Inline(rows...)
    return c.Send("Select monitor:", menu)
}

func (b *Bot) handleShowMonitor(c tele.Context) error {
    monitorID := c.Callback().Data
    pings, _ := b.telegramRepo.GetRecentPings(context.Background(), monitorID, 5)
    var sb strings.Builder
    for _, p := range pings { sb.WriteString(fmt.Sprintf("[%s] %v | %dms\n", p.Timestamp.Format("15:04"), p.Success, p.LatencyMs)) }
    menu := &tele.ReplyMarkup{}
    menu.Inline(menu.Row(menu.Data("Toggle Notify", "toggle_notify", monitorID)))
    return c.Send(sb.String(), menu)
}

func (b *Bot) handleCredits(c tele.Context) error {
    tu, _ := b.telegramRepo.GetTelegramUserByChatID(context.Background(), c.Sender().ID)
    status, _ := b.telegramService.GetCreditStatus(context.Background(), tu.UserID)
    return c.Send(fmt.Sprintf("Credits: %d", status.TotalCreditsLeft))
}

func (b *Bot) handleStatus(c tele.Context) error {
    summaries, _ := b.telegramRepo.GetUserMonitorSummaries(context.Background(), c.Sender().ID)
    return c.Send(fmt.Sprintf("Total Monitors: %d", len(summaries)))
}

// bot/bot.go

// SendMessage implements the services.MessageSender interface.
func (b *Bot) SendMessage(ctx context.Context, chatID int64, text string) error {
    // telebot.v3 doesn't strictly require context for Send,
    // but the interface requires it.
    _, err := b.tele.Send(&tele.Chat{ID: chatID}, text, tele.ModeMarkdown)
    return err
}
