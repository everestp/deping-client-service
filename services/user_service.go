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
}

// =========================
// Implementation
// =========================

type userService struct {
	repo      repositories.UserRepository
	jwtSecret []byte
}

func NewUserService(repo repositories.UserRepository, jwtSecret string) UserService {
	return &userService{
		repo:      repo,
		jwtSecret: []byte(jwtSecret),
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
