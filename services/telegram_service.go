package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"

	"github.com/everestp/deping-client-service/db/repositories"
	"github.com/everestp/deping-client-service/dto"
)

// ═══════════════════════════════════════════════════════════════════════════
// TelegramService Interface
// ═══════════════════════════════════════════════════════════════════════════

type TelegramService interface {
	// InitiateLink generates a verification code and stores it.
	// Called from the web dashboard when the user clicks "Link Telegram".
	InitiateLink(ctx context.Context, userID int, username string) (*dto.LinkTelegramResponse, error)

	// VerifyLink is called by the bot when user sends /verify <code>.
	// Returns the user_id on success so the bot can welcome the user by name.
	VerifyLink(ctx context.Context, code string, chatID int64, chatUsername string) (int, error)

	// GetCreditStatus returns the current credit breakdown for a user.
	GetCreditStatus(ctx context.Context, userID int) (*dto.TelegramCreditStatus, error)

	// AddPurchasedCredits is called after a Solana SPL payment is confirmed.
	AddPurchasedCredits(ctx context.Context, userID int, amount int, txSig string) (int, error)

	// ToggleNotification enables or disables alerts for a specific monitor.
	ToggleNotification(ctx context.Context, userID int, monitorID string, enabled bool) error

	// GetNotificationStatus returns the current toggle for a monitor.
	GetNotificationStatus(ctx context.Context, userID int, monitorID string) (bool, error)

	// GetUserIDFromChatID resolves a Telegram chat ID to a platform user ID.
	GetUserIDFromChatID(ctx context.Context, chatID int64) (int, error)
}

// ═══════════════════════════════════════════════════════════════════════════
// Implementation
// ═══════════════════════════════════════════════════════════════════════════

type telegramService struct {
	repo        repositories.TelegramRepository
	botUsername string // e.g. "YourMonitorBot" — for deeplink in response
	log         *slog.Logger
}

func NewTelegramService(
	repo repositories.TelegramRepository,
	botUsername string,
	log *slog.Logger,
) TelegramService {
	return &telegramService{repo: repo, botUsername: botUsername, log: log}
}

func (s *telegramService) InitiateLink(ctx context.Context, userID int, username string) (*dto.LinkTelegramResponse, error) {
	code, err := generateVerificationCode()
	if err != nil {
		return nil, fmt.Errorf("generate code: %w", err)
	}

	if err := s.repo.UpsertTelegramUser(ctx, userID, username, code); err != nil {
		return nil, fmt.Errorf("upsert telegram user: %w", err)
	}

	s.log.Info("telegram link initiated", "user_id", userID, "username", username)

	return &dto.LinkTelegramResponse{
		VerificationCode: code,
		BotUsername:      "@" + s.botUsername,
		Message: fmt.Sprintf(
			"Open Telegram and send this command to %s:\n\n/verify %s\n\nThe code expires when you use it.",
			"@"+s.botUsername, code,
		),
	}, nil
}

func (s *telegramService) VerifyLink(ctx context.Context, code string, chatID int64, chatUsername string) (int, error) {
	tu, err := s.repo.GetTelegramUserByCode(ctx, code)
	if err != nil {
		return 0, fmt.Errorf("lookup code: %w", err)
	}
	if tu == nil {
		return 0, fmt.Errorf("invalid or expired verification code")
	}

	if err := s.repo.VerifyTelegramUser(ctx, tu.UserID, chatID, chatUsername); err != nil {
		return 0, fmt.Errorf("verify user: %w", err)
	}

	s.log.Info("telegram account verified", "user_id", tu.UserID, "chat_id", chatID)
	return tu.UserID, nil
}

func (s *telegramService) GetCreditStatus(ctx context.Context, userID int) (*dto.TelegramCreditStatus, error) {
	c, err := s.repo.GetCreditStatus(ctx, userID)
	if err != nil {
		return nil, err
	}

	const dailyCap = 3
	freeLeft := dailyCap - c.FreeCreditsUsed
	if freeLeft < 0 {
		freeLeft = 0
	}

	return &dto.TelegramCreditStatus{
		FreeCreditsUsed:  c.FreeCreditsUsed,
		FreeCreditsLeft:  freeLeft,
		FreeResetDate:    c.FreeResetDate,
		PurchasedCredits: c.PurchasedCredits,
		TotalCreditsLeft: freeLeft + c.PurchasedCredits,
	}, nil
}

func (s *telegramService) AddPurchasedCredits(ctx context.Context, userID int, amount int, txSig string) (int, error) {
	return s.repo.AddPurchasedCredits(ctx, userID, amount, txSig)
}

func (s *telegramService) ToggleNotification(ctx context.Context, userID int, monitorID string, enabled bool) error {
	return s.repo.UpsertSubscription(ctx, userID, monitorID, enabled)
}

func (s *telegramService) GetNotificationStatus(ctx context.Context, userID int, monitorID string) (bool, error) {
	sub, err := s.repo.GetSubscription(ctx, userID, monitorID)
	if err != nil {
		return false, err
	}
	if sub == nil {
		return true, nil // default: enabled
	}
	return sub.IsNotificationsEnabled, nil
}

func (s *telegramService) GetUserIDFromChatID(ctx context.Context, chatID int64) (int, error) {
	tu, err := s.repo.GetTelegramUserByChatID(ctx, chatID)
	if err != nil {
		return 0, err
	}
	if tu == nil {
		return 0, fmt.Errorf("telegram account not linked")
	}
	return tu.UserID, nil
}

// generateVerificationCode creates a cryptographically secure 6-character hex code.
func generateVerificationCode() (string, error) {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b)[:8], nil // 8 hex chars = visually distinctive
}
