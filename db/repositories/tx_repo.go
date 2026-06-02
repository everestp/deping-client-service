package repositories

import (
	"context"
	"database/sql"
	"fmt"
)

type TransactionRepository interface {
	// ProcessPayment atomically updates balance and marks tx as processed
	ProcessPayment(ctx context.Context, userID int, amount uint64, txSig string) error
	IsTxProcessed(ctx context.Context, txSig string) (bool, error)
}

type postgresTxRepo struct {
	db *sql.DB
}

func NewTransactionRepository(db *sql.DB) TransactionRepository {
	return &postgresTxRepo{db: db}
}

// ProcessPayment uses a SQL transaction to prevent double-spending
func (r *postgresTxRepo) ProcessPayment(ctx context.Context, userID int, amount uint64, txSig string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	// Defer rollback in case of error
	defer tx.Rollback()

	// 1. Mark transaction as processed
	// If txSig exists, this will return an error due to the UNIQUE constraint
	_, err = tx.ExecContext(ctx,
		"INSERT INTO processed_transactions (signature, user_id, amount) VALUES ($1, $2, $3)",
		txSig, userID, amount)
	if err != nil {
		return fmt.Errorf("transaction already processed or database error: %w", err)
	}

	// 2. Update user credits
	_, err = tx.ExecContext(ctx,
		"UPDATE users SET credits = credits + $1 WHERE id = $2",
		amount, userID)
	if err != nil {
		return err
	}

	// 3. Commit
	return tx.Commit()
}

func (r *postgresTxRepo) IsTxProcessed(ctx context.Context, txSig string) (bool, error) {
	var exists bool
	query := "SELECT EXISTS(SELECT 1 FROM processed_transactions WHERE signature = $1)"
	err := r.db.QueryRowContext(ctx, query, txSig).Scan(&exists)
	return exists, err
}
