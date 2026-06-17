package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/everestp/deping-client-service/config/env"
	"github.com/everestp/deping-client-service/db/repositories"
	"github.com/everestp/deping-client-service/dto"
	"github.com/everestp/deping-client-service/solana"
)

// =========================
// Sentinel errors
// =========================

var (
	ErrEmailTaken  = errors.New("email already in use")
	ErrWalletTaken = errors.New("wallet already in use")
	ErrNotFound    = errors.New("user not found")
	ErrBadPassword = errors.New("invalid credentials")
	ErrInvalidToken = errors.New("invalid token")
	ErrExpiredToken = errors.New("token expired")
)

// =========================
// Interface
// =========================

type UserService interface {
	Register(ctx context.Context, req dto.RegisterRequest) (*dto.AuthResponse, error)
	Login(ctx context.Context, req dto.LoginRequest) (*dto.AuthResponse, error)
	GetUserInfo(ctx context.Context, userID int) (*dto.UserInfo, error)
	ValidateToken(tokenStr string) (int, error)
	AddMonitorPurchasedCredits(ctx context.Context, userID int, amount int, txSig string, creditBalance int) (int, error)

}

// =========================
// Implementation
// =========================

type userService struct {
	repo      repositories.UserRepository
	jwtSecret []byte
	tx_repo repositories.TransactionRepository
	solanaClient *solana.Client
	 log           *slog.Logger
}

func NewUserService(_repo repositories.UserRepository, jwtSecret string, _tx_repo repositories.TransactionRepository,log *slog.Logger , solanaClient *solana.Client) UserService {
	return &userService{
		repo:      _repo,
		jwtSecret: []byte(jwtSecret),
		tx_repo: _tx_repo,
		solanaClient: solanaClient,

	}
}

// =========================
// REGISTER
// =========================

func (s *userService) Register(ctx context.Context, req dto.RegisterRequest) (*dto.AuthResponse, error) {
	if req.Email == "" || req.Password == "" || req.WalletPubkey == "" {
		return nil, fmt.Errorf("all fields are required")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	user, err := s.repo.CreateUser(ctx, req.Email, string(hash), req.WalletPubkey)
	if err != nil {
		// Prefer repository typed errors (recommended)
		switch {
		case errors.Is(err, repositories.ErrDuplicateEmail):
			return nil, ErrEmailTaken
		case errors.Is(err, repositories.ErrDuplicateWallet):
			return nil, ErrWalletTaken
		default:
			return nil, fmt.Errorf("create user: %w", err)
		}
	}

	token, err := s.generateToken(user.ID, user.Email)
	if err != nil {
		return nil, fmt.Errorf("generate token: %w", err)
	}

	return &dto.AuthResponse{
		Token: token,
		User: dto.UserInfo{
			ID:           user.ID,
			Email:        user.Email,
			WalletPubkey: user.WalletPubkey,
		},
	}, nil
}

// =========================
// LOGIN
// =========================

func (s *userService) Login(ctx context.Context, req dto.LoginRequest) (*dto.AuthResponse, error) {
	user, err := s.repo.GetUserByEmail(ctx, req.Email)
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}
	if user == nil {
		return nil, ErrBadPassword
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		return nil, ErrBadPassword
	}

	token, err := s.generateToken(user.ID, user.Email)
	if err != nil {
		return nil, fmt.Errorf("generate token: %w", err)
	}

	return &dto.AuthResponse{
		Token: token,
		User: dto.UserInfo{
			ID:           user.ID,
			Email:        user.Email,
			WalletPubkey: user.WalletPubkey,
		},
	}, nil
}

// =========================
// USER INFO
// =========================

func (s *userService) GetUserInfo(ctx context.Context, userID int) (*dto.UserInfo, error) {
	user, err := s.repo.GetUserByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}
	if user == nil {
		return nil, ErrNotFound
	}

	return &dto.UserInfo{
		ID:           user.ID,
		Email:        user.Email,
		WalletPubkey: user.WalletPubkey,
		MonitorCreditBalance : user.MonitorCreditBalance,
	}, nil
}

// =========================
// TOKEN VALIDATION
// =========================

func (s *userService) ValidateToken(tokenStr string) (int, error) {
	token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return s.jwtSecret, nil
	})

	if err != nil || !token.Valid {
		return 0, ErrInvalidToken
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return 0, ErrInvalidToken
	}

	// check expiration
	exp, ok := claims["exp"].(float64)
	if !ok {
		return 0, ErrInvalidToken
	}
	if int64(exp) < time.Now().Unix() {
		return 0, ErrExpiredToken
	}

	// safe subject parsing
	sub, ok := claims["sub"]
	if !ok {
		return 0, ErrInvalidToken
	}

	var userID int
	switch v := sub.(type) {
	case float64:
		userID = int(v)
	case int:
		userID = v
	case int64:
		userID = int(v)
	default:
		return 0, ErrInvalidToken
	}

	return userID, nil
}

// =========================
// JWT GENERATOR
// =========================

func (s *userService) generateToken(userID int, email string) (string, error) {
	claims := jwt.MapClaims{
		"sub":   userID,
		"email": email,
		"iat":   time.Now().Unix(),
		"exp":   time.Now().Add(7 * 24 * time.Hour).Unix(),
		"iss":   "deping-client-service",
		"aud":   "deping-app",
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(s.jwtSecret)
}

func (s *userService) AddMonitorPurchasedCredits(
	ctx context.Context,
	userID int,
	amount int,
	txSig string,
    creditBalance int,
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
   
   amount , err = s.repo.AddMonitorPurchasedCredits(ctx,userID,creditBalance,txSig)
  
  	if err != nil {
		return 0, fmt.Errorf("failed to  update DB: %w", err)
	}
	return amount, nil
}
