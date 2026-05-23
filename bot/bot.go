package bot

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode"

	tele "gopkg.in/telebot.v3"
	"gopkg.in/telebot.v3/middleware"

	"github.com/everestp/deping-client-service/db/repositories"
	"github.com/everestp/deping-client-service/dto"
	"github.com/everestp/deping-client-service/services"
)

// ═══════════════════════════════════════════════════════════════════════════
// Bot
// ═══════════════════════════════════════════════════════════════════════════

// Bot wraps telebot.v3 with application services.
// It also implements services.MessageSender so AlertService can send messages
// without importing the bot package (avoiding circular imports).
type Bot struct {
	tele            *tele.Bot
	telegramService services.TelegramService
	alertService    services.AlertService
	telegramRepo    repositories.TelegramRepository
	log             *slog.Logger
}

// NewBot creates and wires the Telegram bot.
// alertService may be nil at construction time; call SetAlertService before Start.
func NewBot(
	token string,
	telegramService services.TelegramService,
	alertService services.AlertService,
	telegramRepo repositories.TelegramRepository,
	log *slog.Logger,
) (*Bot, error) {
	pref := tele.Settings{
		Token:  token,
		Poller: &tele.LongPoller{Timeout: 10 * time.Second},
	}
	b, err := tele.NewBot(pref)
	if err != nil {
		return nil, fmt.Errorf("create telebot: %w", err)
	}

	bot := &Bot{
		tele:            b,
		telegramService: telegramService,
		alertService:    alertService,
		telegramRepo:    telegramRepo,
		log:             log,
	}
	bot.registerHandlers()
	return bot, nil
}

// SetAlertService injects the alert service after construction
// (breaks the circular dependency in application.go).
func (b *Bot) SetAlertService(svc services.AlertService) {
	b.alertService = svc
}

// Start begins long-polling. Call in a goroutine.
func (b *Bot) Start() {
	b.log.Info("Telegram bot starting")
	b.tele.Start()
}

// Stop gracefully shuts down the bot.
func (b *Bot) Stop() {
	b.tele.Stop()
}

// SendMessage implements services.MessageSender.
// Used by AlertService to send notifications without importing this package.
func (b *Bot) SendMessage(_ context.Context, chatID int64, text string) error {
	chat := &tele.Chat{ID: chatID}
	_, err := b.tele.Send(chat, text, tele.ModeMarkdown)
	return err
}

// ═══════════════════════════════════════════════════════════════════════════
// Handler Registration
// ═══════════════════════════════════════════════════════════════════════════

func (b *Bot) registerHandlers() {
	b.tele.Use(middleware.Recover())

	// Public
	b.tele.Handle("/start", b.handleStart)
	b.tele.Handle("/help", b.handleHelp)
	b.tele.Handle("/verify", b.handleVerify)

	// Authenticated
	b.tele.Handle("/monitor", b.withAuth(b.handleMonitor))
	b.tele.Handle("/credits", b.withAuth(b.handleCredits))
	b.tele.Handle("/status", b.withAuth(b.handleStatus))

	// Inline keyboard callbacks
	showMonitorBtn := tele.Btn{Unique: "show_monitor"}
	toggleNotifyBtn := tele.Btn{Unique: "toggle_notify"}

	b.tele.Handle(&showMonitorBtn, b.withAuth(b.handleShowMonitor))
	b.tele.Handle(&toggleNotifyBtn, b.withAuth(b.handleToggleNotify))

	b.tele.Handle(tele.OnText, b.handleText)
}

// withAuth wraps a handler to require a verified linked account.
func (b *Bot) withAuth(next tele.HandlerFunc) tele.HandlerFunc {
	return func(c tele.Context) error {
		ctx := context.Background()
		tu, err := b.telegramRepo.GetTelegramUserByChatID(ctx, c.Sender().ID)
		if err != nil || tu == nil || !tu.IsVerified {
			return c.Send(
				"🔐 Your account isn't linked yet\\.\n\nGo to your *dashboard → Telegram* section to get a verification code, then send:\n`/verify <your\\-code>`",
				tele.ModeMarkdownV2,
			)
		}
		return next(c)
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// Command Handlers
// ═══════════════════════════════════════════════════════════════════════════

func (b *Bot) handleStart(c tele.Context) error {
    ctx := context.Background()
    // Check if this Telegram ID is already linked
    tu, _ := b.telegramRepo.GetTelegramUserByChatID(ctx, c.Sender().ID)

    if tu != nil && tu.IsVerified {
        return c.Send("✅ *Connected to deping\\.xyz!*\n\nYour account is already linked and ready to receive updates\\. Use /monitor to see your infrastructure status\\.", tele.ModeMarkdownV2)
    }

    // If not linked, give them the specific link to their dashboard
    return c.Send(`👋 *Welcome to the deping.xyz Monitor Bot*

To receive environmental and system alerts, you need to link your Telegram account to your dashboard:

1️⃣ Go to [deping.xyz/settings](https://deping.xyz/settings)
2️⃣ Find the *Telegram Integration* section
3️⃣ Enter your username: @` + c.Sender().Username + `
4️⃣ Copy your verification code
5️⃣ Send it here: /verify <code>`, tele.ModeMarkdownV2)
}
func (b *Bot) handleHelp(c tele.Context) error {
	return c.Send(`📖 *UptimeMonitor Bot — All Commands*

━━━━━━━━━━━━━━━━━
🔐 *Setup*
/start — Welcome & linking guide
/verify \<code\> — Link your dashboard account

━━━━━━━━━━━━━━━━━
📊 *Monitoring*
/monitor — All your sites with live status
/status — Quick up/down/paused overview

━━━━━━━━━━━━━━━━━
💳 *Credits*
/credits — Check balance & top\-up info

━━━━━━━━━━━━━━━━━
ℹ️ *How alerts work*

✦ *Two\-check rule* — A site is only flagged DOWN after 2 consecutive failed checks\. No false alarms\.
✦ *Smart recovery* — "Restored" alert only fires if we previously sent a "Down" alert\.
✦ *Free tier* — 3 alert messages per day \(resets midnight UTC\)\.
✦ *Premium* — Buy credits from dashboard using SOL tokens\.

━━━━━━━━━━━━━━━━━
⚙️ *Notification control*
Toggle alerts per\-monitor from the dashboard → Monitor settings, or tap the button shown in \/monitor\.

━━━━━━━━━━━━━━━━━
💡 *Tips*
• Tap any monitor in \/monitor to see last 5 ping details with phase breakdown
• If you change Telegram username, re\-link from the dashboard`, tele.ModeMarkdownV2)
}

func (b *Bot) handleVerify(c tele.Context) error {
	args := c.Args()
	if len(args) == 0 {
		return c.Send("❌ Please include your code:\n`/verify <code>`", tele.ModeMarkdown)
	}
	code := strings.TrimSpace(args[0])

	ctx := context.Background()
	username := c.Sender().Username
	userID, err := b.telegramService.VerifyLink(ctx, code, c.Sender().ID, username)
	if err != nil {
		b.log.Warn("verification failed", "chat_id", c.Sender().ID, "err", err)
		return c.Send("❌ *Verification failed\\.* The code is invalid or already used\\.\n\nGenerate a fresh code from your dashboard → Telegram settings\\.", tele.ModeMarkdownV2)
	}
	_ = userID

	return c.Send(fmt.Sprintf(`✅ *Account linked\!* Welcome, *%s\!* 🎉

Your Telegram is now connected\. You'll receive alerts here when monitors detect issues\.

*Free plan:* 3 alert messages per day
*Protection:* 2\-check false\-alarm prevention

Send \/monitor to see your sites\.`, escapeMarkdownV2(c.Sender().FirstName)), tele.ModeMarkdownV2)
}

func (b *Bot) handleMonitor(c tele.Context) error {
	ctx := context.Background()
	summaries, err := b.telegramRepo.GetUserMonitorSummaries(ctx, c.Sender().ID)
	if err != nil {
		b.log.Error("get monitor summaries", "err", err)
		return c.Send("⚠️ Could not fetch monitors. Try again shortly.")
	}

	if len(summaries) == 0 {
		return c.Send("📭 *No monitors found\\.*\n\nHead to your dashboard to add your first website\\!", tele.ModeMarkdownV2)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("📊 *Your Monitors* \\(%d total\\)\n\n", len(summaries)))

	menu := &tele.ReplyMarkup{}
	var rows []tele.Row

	for _, s := range summaries {
		statusEmoji, statusText := monitorStatusEmoji(s)

		displayURL := truncateURL(s.TargetURL, 38)
		sb.WriteString(fmt.Sprintf("%s `%s`\n   └ %s • %.1f%% uptime • avg %dms\n\n",
			statusEmoji,
			escapeMarkdownV2(displayURL),
			statusText,
			s.UptimePct,
			int(s.AvgLatency),
		))

		btn := menu.Data(
			fmt.Sprintf("%s %s", statusEmoji, displayURL),
			"show_monitor",
			s.ID,
		)
		rows = append(rows, menu.Row(btn))
	}

	menu.Inline(rows...)
	return c.Send(sb.String(), menu, tele.ModeMarkdownV2)
}

func (b *Bot) handleShowMonitor(c tele.Context) error {
	monitorID := strings.TrimSpace(c.Callback().Data)
	_ = c.Respond()

	ctx := context.Background()
	pings, err := b.telegramRepo.GetRecentPings(ctx, monitorID, 5)
	if err != nil {
		return c.Send("⚠️ Error fetching ping data.")
	}

	if len(pings) == 0 {
		return c.Send("📭 No ping data recorded yet for this monitor.")
	}

	var sb strings.Builder
	sb.WriteString("📈 *Last 5 Checks*\n\n")

	for _, p := range pings {
		icon := "✅"
		statusDesc := fmt.Sprintf("HTTP %d", p.StatusCode)
		if !p.Success {
			icon = "❌"
			if p.ErrorKind != "" {
				statusDesc = p.ErrorKind
			}
		}

		ts := p.Timestamp.Format("Jan 02 15:04")
		sb.WriteString(fmt.Sprintf(
			"%s *%s* — `%s`\n"+
				"   ⚡ `%dms` total  │  TTFB `%dms`\n"+
				"   🔍 DNS `%dµs` · TCP `%dµs` · TLS `%dµs`\n"+
				"   🌍 %s\n\n",
			icon,
			escapeMarkdownV2(ts),
			escapeMarkdownV2(statusDesc),
			p.LatencyMs,
			int(p.TtfbUs/1000),
			p.DnsUs, p.TcpUs, p.TlsUs,
			escapeMarkdownV2(p.GeoRegion),
		))
	}

	menu := &tele.ReplyMarkup{}
	toggleBtn := menu.Data("🔔 Toggle Alerts For This Monitor", "toggle_notify", monitorID)
	menu.Inline(menu.Row(toggleBtn))

	return c.Send(sb.String(), menu, tele.ModeMarkdownV2)
}

func (b *Bot) handleToggleNotify(c tele.Context) error {
	monitorID := strings.TrimSpace(c.Callback().Data)
	_ = c.Respond()

	ctx := context.Background()
	tu, _ := b.telegramRepo.GetTelegramUserByChatID(ctx, c.Sender().ID)
	if tu == nil {
		return c.Send("❌ Account not linked.")
	}

	sub, err := b.telegramRepo.GetSubscription(ctx, tu.UserID, monitorID)
	if err != nil {
		return c.Send("⚠️ Error fetching subscription.")
	}

	currentEnabled := true
	if sub != nil {
		currentEnabled = sub.IsNotificationsEnabled
	}
	newEnabled := !currentEnabled

	if err := b.telegramRepo.UpsertSubscription(ctx, tu.UserID, monitorID, newEnabled); err != nil {
		return c.Send("⚠️ Could not update notification preference.")
	}

	icon, state := "🔔", "ENABLED"
	if !newEnabled {
		icon, state = "🔕", "DISABLED"
	}

	return c.Send(fmt.Sprintf("%s Notifications for this monitor are now *%s*\\.\n\nYou can also manage this from your web dashboard\\.",
		icon, state), tele.ModeMarkdownV2)
}

func (b *Bot) handleCredits(c tele.Context) error {
	ctx := context.Background()
	tu, _ := b.telegramRepo.GetTelegramUserByChatID(ctx, c.Sender().ID)
	if tu == nil {
		return c.Send("❌ Account not linked.")
	}

	status, err := b.telegramService.GetCreditStatus(ctx, tu.UserID)
	if err != nil {
		return c.Send("⚠️ Could not fetch credit status.")
	}

	resetStr := status.FreeResetDate.Add(24 * time.Hour).Format("Jan 02 at 00:00 UTC")

	urgencyMsg := ""
	switch {
	case status.TotalCreditsLeft == 0:
		urgencyMsg = "\n\n⚠️ *No credits left\\!* Alerts are paused\\. Top up to resume\\."
	case status.TotalCreditsLeft <= 2:
		urgencyMsg = "\n\n💡 *Running low\\!* Consider topping up to avoid missing alerts\\."
	}

	return c.Send(fmt.Sprintf(`💳 *Notification Credits*

━━━━━━━━━━━━━━━━━
🆓 *Free daily:*   %d / 3 used  →  *%d left*
📅 Resets: %s

💰 *Purchased:*  %d credits
━━━━━━━━━━━━━━━━━
📊 *Total available:* *%d credits*%s

━━━━━━━━━━━━━━━━━
*Buy more credits:*
Dashboard → Telegram section
Pay with SOL tokens \(100 credits = 100 tokens\)

Each alert message deducts 1 credit \(free tier used first\)\.`,
		status.FreeCreditsUsed, status.FreeCreditsLeft,
		escapeMarkdownV2(resetStr),
		status.PurchasedCredits,
		status.TotalCreditsLeft,
		urgencyMsg,
	), tele.ModeMarkdownV2)
}

func (b *Bot) handleStatus(c tele.Context) error {
	ctx := context.Background()
	summaries, err := b.telegramRepo.GetUserMonitorSummaries(ctx, c.Sender().ID)
	if err != nil {
		return c.Send("⚠️ Could not fetch monitor status.")
	}

	total, down, up, paused := len(summaries), 0, 0, 0
	for _, s := range summaries {
		switch {
		case s.IsDown:
			down++
		case !s.IsActive:
			paused++
		default:
			up++
		}
	}

	overallIcon := "🟢"
	if down > 0 {
		overallIcon = "🔴"
	}

	return c.Send(fmt.Sprintf(`%s *Monitor Overview*

📊 Total:    %d
✅ Up:       %d
🔴 Down:     %d
⏸ Paused:   %d

_Checked: %s UTC_

Tap \/monitor for details & ping history\.`,
		overallIcon, total, up, down, paused,
		escapeMarkdownV2(time.Now().UTC().Format("Jan 02 15:04")),
	), tele.ModeMarkdownV2)
}

func (b *Bot) handleText(c tele.Context) error {
	if strings.HasPrefix(c.Text(), "/") {
		return nil // unknown command — silently ignore
	}
	return c.Send("👋 Use a command to interact with me\\. Send /help to see what I can do\\!", tele.ModeMarkdownV2)
}

// ═══════════════════════════════════════════════════════════════════════════
// Helpers
// ═══════════════════════════════════════════════════════════════════════════

func monitorStatusEmoji(s dto.MonitorSummary) (string, string) {
	switch {
	case s.IsDown:
		return "🔴", "DOWN"
	case !s.IsActive:
		return "⏸", "PAUSED"
	default:
		return "🟢", "UP"
	}
}

func truncateURL(url string, max int) string {
	url = strings.TrimPrefix(url, "https://")
	url = strings.TrimPrefix(url, "http://")
	url = strings.TrimSuffix(url, "/")
	if len(url) <= max {
		return url
	}
	return url[:max-1] + "…"
}

// escapeMarkdownV2 escapes special characters for Telegram MarkdownV2.
func escapeMarkdownV2(s string) string {
	var sb strings.Builder
	for _, r := range s {
		if strings.ContainsRune(`_*[]()~` + "`" + `>#+-=|{}.!`, r) {
			sb.WriteRune('\\')
		}
		sb.WriteRune(r)
	}
	return sb.String()
}

// sanitizeURLToSlug converts a URL to a valid identifier (unused but useful for debugging).
func sanitizeURLToSlug(url string) string {
	url = strings.TrimPrefix(url, "https://")
	url = strings.TrimPrefix(url, "http://")
	var sb strings.Builder
	for _, r := range url {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			sb.WriteRune(unicode.ToLower(r))
		} else {
			sb.WriteRune('_')
		}
	}
	return sb.String()
}
