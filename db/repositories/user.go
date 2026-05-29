package repositories

import (
	"context"
	"database/sql"
	"errors"
)

// User domain model
type User struct {
	ID           int
	Email        string
	PasswordHash string
	WalletPubkey string
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

func (r *postgresUserRepo) GetUserByID(ctx context.Context, id int) (*User, error) {
	const q = `
		SELECT id, email, password_hash, wallet_pubkey
		FROM users WHERE id = $1 AND deleted_at IS NULL`
	u := &User{}
	err := r.db.QueryRowContext(ctx, q, id).
		Scan(&u.ID, &u.Email, &u.PasswordHash, &u.WalletPubkey)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return u, err
}
