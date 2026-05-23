package services

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/everestp/deping-client-service/db/repositories"
	"github.com/everestp/deping-client-service/dto"
)

// Sentinel errors — checked by the controller for proper HTTP status codes
var (
	ErrEmailTaken  = errors.New("email already in use")
	ErrWalletTaken = errors.New("wallet already in use")
	ErrNotFound    = errors.New("user not found")
	ErrBadPassword = errors.New("invalid credentials")
)

// ═══════════════════════════════════════════════════════════════════════════
// UserService Interface
// ═══════════════════════════════════════════════════════════════════════════

type UserService interface {
	Register(ctx context.Context, req dto.RegisterRequest) (*dto.AuthResponse, error)
	Login(ctx context.Context, req dto.LoginRequest) (*dto.AuthResponse, error)
	GetUserInfo(ctx context.Context, userID int) (*dto.UserInfo, error)
	ValidateToken(tokenStr string) (int, error)
}

// ═══════════════════════════════════════════════════════════════════════════
// Implementation
// ═══════════════════════════════════════════════════════════════════════════

type userService struct {
	repo      repositories.UserRepository
	jwtSecret []byte
}

func NewUserService(repo repositories.UserRepository, jwtSecret string) UserService {
	return &userService{repo: repo, jwtSecret: []byte(jwtSecret)}
}

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
		// Postgres unique violation — check column name in error
		errStr := err.Error()
		switch {
		case containsStr(errStr, "users_email_key") || containsStr(errStr, "email"):
			return nil, ErrEmailTaken
		case containsStr(errStr, "wallet_pubkey") || containsStr(errStr, "wallet"):
			return nil, ErrWalletTaken
		}
		return nil, fmt.Errorf("create user: %w", err)
	}

	token, err := s.generateToken(user.ID)
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

func (s *userService) Login(ctx context.Context, req dto.LoginRequest) (*dto.AuthResponse, error) {
	user, err := s.repo.GetUserByEmail(ctx, req.Email)
	if err != nil || user == nil {
		return nil, ErrBadPassword
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		return nil, ErrBadPassword
	}

	token, err := s.generateToken(user.ID)
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

func (s *userService) GetUserInfo(ctx context.Context, userID int) (*dto.UserInfo, error) {
	user, err := s.repo.GetUserByID(ctx, userID)
	if err != nil || user == nil {
		return nil, ErrNotFound
	}
	return &dto.UserInfo{
		ID:           user.ID,
		Email:        user.Email,
		WalletPubkey: user.WalletPubkey,
	}, nil
}

func (s *userService) ValidateToken(tokenStr string) (int, error) {
	token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return s.jwtSecret, nil
	})
	if err != nil || !token.Valid {
		return 0, fmt.Errorf("invalid token")
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return 0, fmt.Errorf("invalid claims")
	}

	sub, ok := claims["sub"].(float64)
	if !ok {
		return 0, fmt.Errorf("invalid subject")
	}
	return int(sub), nil
}

func (s *userService) generateToken(userID int) (string, error) {
	claims := jwt.MapClaims{
		"sub": userID,
		"iat": time.Now().Unix(),
		"exp": time.Now().Add(7 * 24 * time.Hour).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(s.jwtSecret)
}

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr ||
		len(s) > 0 && func() bool {
			for i := 0; i <= len(s)-len(substr); i++ {
				if s[i:i+len(substr)] == substr {
					return true
				}
			}
			return false
		}())
}
