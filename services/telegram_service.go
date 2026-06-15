package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"

	"github.com/everestp/deping-client-service/config/env"
	"github.com/everestp/deping-client-service/db/repositories"
	"github.com/everestp/deping-client-service/dto"
	"github.com/everestp/deping-client-service/solana"
)
type TelegramService interface {
    InitiateLink(ctx context.Context, userID int, username string) (*dto.LinkTelegramResponse, error)
    VerifyLink(ctx context.Context, code string, chatID int64, chatUsername string) (int, error)
    GetCreditStatus(ctx context.Context, userID int) (*dto.TelegramCreditStatus, error)
    AddPurchasedCredits(ctx context.Context, userID int, amount int, txSig string) (int, error)
    ToggleNotification(ctx context.Context, userID int, monitorID string, enabled bool) error
    GetNotificationStatus(ctx context.Context, userID int, monitorID string) (bool, error)
    GetUserIDFromChatID(ctx context.Context, chatID int64) (int, error)
     GetTelegramUserStatus(ctx context.Context, userID int) (*dto.ResponseEnvelope, error)
}

type telegramService struct {
    repo          repositories.TelegramRepository
    tx_repo       repositories.TransactionRepository
    solanaClient  *solana.Client // Explicitly type your client
    botUsername   string
    log           *slog.Logger


}
func NewTelegramService(repo repositories.TelegramRepository, botUsername string, log *slog.Logger , solanaClient *solana.Client) TelegramService {
    return &telegramService{repo: repo, botUsername: botUsername, log: log,solanaClient:solanaClient }
}

func (s *telegramService) InitiateLink(ctx context.Context, userID int, username string) (*dto.LinkTelegramResponse, error) {
    code, err := generateVerificationCode()
    if err != nil {
        return nil, fmt.Errorf("failed to generate code: %w", err)
    }

    // Upsert ensures the user record exists and resets verification status
    if err := s.repo.UpsertTelegramUser(ctx, userID, username, code); err != nil {
        return nil, fmt.Errorf("repository upsert failed: %w", err)
    }

    return &dto.LinkTelegramResponse{
        VerificationCode: code,
        BotUsername:      "@" + s.botUsername,
        Message:          "Verification code generated successfully.",
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
    if freeLeft < 0 { freeLeft = 0 }

    return &dto.TelegramCreditStatus{
        FreeCreditsUsed:  c.FreeCreditsUsed,
        FreeCreditsLeft:  freeLeft,
        FreeResetDate:    c.FreeResetDate,
        PurchasedCredits: c.PurchasedCredits,
        TotalCreditsLeft: freeLeft + c.PurchasedCredits,
    }, nil
}

func (s *telegramService) AddPurchasedCredits(
	ctx context.Context,
	userID int,
	amount int,
	txSig string,
) (int, error) {

	// 1. Prevent duplicate processing
	processed, err := s.tx_repo.IsTxProcessed(ctx, txSig)
	if err != nil {
		return 0, err
	}

	if processed {
		return 0, errors.New("transaction already processed")
	}

	// 2. Fetch blockchain data (MINT-AWARE)
	txInfo, err := s.solanaClient.GetTransferInfo(ctx, txSig)
	if err != nil {
		return 0, fmt.Errorf("failed to verify transaction: %w", err)
	}

	// 3. Treasury wallet (must be receiver)
	treasuryAddress := env.Get().TreasuryOwnerAddr

	if txInfo.Receiver != treasuryAddress {
		return 0, errors.New("payment sent to wrong treasury address")
	}

	// 4. SAFE AMOUNT CHECK (NO FLOAT)
	expected := uint64(amount)

	if uint64(txInfo.Amount) != expected {
		return 0, fmt.Errorf(
			"amount mismatch expected=%d got=%d",
			expected,
			txInfo.Amount,
		)
	}

	// 5. Commit transaction
	err = s.tx_repo.ProcessPayment(
		ctx,
		userID,
		uint64(amount),
		txSig,
	)
	if err != nil {
		return 0, fmt.Errorf("failed to finalize transaction: %w", err)
	}

	return amount, nil
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
        return true, nil
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
func (s *telegramService) GetTelegramUserStatus(ctx context.Context, userID int) (*dto.ResponseEnvelope, error) {

    user, err := s.repo.GetTelegramForUserForMeByUserID(ctx, userID)
    if err != nil {
        // This is an actual DB crash or connection error
        return &dto.ResponseEnvelope{Success: false, Data: nil}, err
    }

    // If user is nil (No rows matched), this successfully signals your frontend
    if user == nil {
        return &dto.ResponseEnvelope{
            Success: true,
            Data:    nil, 
        }, nil
    }

    // If data exists, return it inside the data wrapper
    return &dto.ResponseEnvelope{
        Success: true,
        Data:    user,
    }, nil
}

func generateVerificationCode() (string, error) {
    b := make([]byte, 4)
    if _, err := rand.Read(b); err != nil {
        return "", err
    }
    return hex.EncodeToString(b), nil // 8 character hex string
}
