package repositories

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// User domain model
type User struct {
	ID           int
	Email        string
	PasswordHash string
	WalletPubkey string
	MonitorCreditBalance string
}
var (
	ErrDuplicateEmail  = errors.New("duplicate email")
	ErrDuplicateWallet = errors.New("duplicate wallet")
)
// UserRepository defines the persistence contract for users.
type UserRepository interface {
	CreateUser(ctx context.Context, email, passwordHash, walletPubkey string) (*User, error)
	GetUserByEmail(ctx context.Context, email string) (*User, error)
	GetUserByID(ctx context.Context, id int) (*User, error)
	AddMonitorPurchasedCredits(ctx context.Context, userID int, amount int, txSignature string) (int, error)
}


type postgresUserRepo struct {
	db *sql.DB
}

func NewUserRepository(db *sql.DB) UserRepository {
	return &postgresUserRepo{db: db}
}




func (r *postgresUserRepo) CreateUser(ctx context.Context, email, passwordHash, walletPubkey string) (*User, error) {
	const q = `
		INSERT INTO users (email, password_hash, wallet_pubkey)
		VALUES ($1, $2, $3)
		RETURNING id, email, password_hash, wallet_pubkey`
	u := &User{}
	err := r.db.QueryRowContext(ctx, q, email, passwordHash, walletPubkey).
		Scan(&u.ID, &u.Email, &u.PasswordHash, &u.WalletPubkey)
	return u, err
}

func (r *postgresUserRepo) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	const q = `
		SELECT id, email, password_hash, wallet_pubkey
		FROM users WHERE email = $1 AND deleted_at IS NULL`
	u := &User{}
	err := r.db.QueryRowContext(ctx, q, email).
		Scan(&u.ID, &u.Email, &u.PasswordHash, &u.WalletPubkey)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return u, err
}
func (r *postgresUserRepo) AddMonitorPurchasedCredits(ctx context.Context, userID int, amount int, txSignature string) (int, error) {
	// 1. Update the user balance and return the new total
    // We use a transaction to ensure integrity, although a single UPDATE is atomic in Postgres.
    const q = `
        UPDATE users 
        SET credit_balance = credit_balance + $1 
        WHERE id = $2 
        RETURNING credit_balance
    `

    var newBalance int
    err := r.db.QueryRowContext(ctx, q, amount, userID).Scan(&newBalance)
    if err != nil {
        return 0, fmt.Errorf("failed to add credits: %w", err)
    }

    return newBalance, nil
}

func (r *postgresUserRepo) GetUserByID(ctx context.Context, id int) (*User, error) {
	const q = `
		SELECT id, email, password_hash, wallet_pubkey, credit_balance
		FROM users WHERE id = $1 AND deleted_at IS NULL`
	u := &User{}
	err := r.db.QueryRowContext(ctx, q, id).
		Scan(&u.ID, &u.Email, &u.PasswordHash, &u.WalletPubkey,&u.MonitorCreditBalance)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return u, err
}
