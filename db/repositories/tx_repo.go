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

// ProcessPayment marks a transaction as processed to prevent replay attacks.
func (r *postgresTxRepo) ProcessPayment(ctx context.Context, userID int, amount uint64, txSig string) error {
    // We no longer need BeginTx/Commit because we are executing a single atomic INSERT.
    // The PRIMARY KEY (signature) will throw an error if the record already exists.
    query := `
        INSERT INTO processed_transactions (signature, user_id, amount, timestamp_ms) 
        VALUES ($1, $2, $3, EXTRACT(EPOCH FROM NOW()) * 1000)`
    
    _, err := r.db.ExecContext(ctx, query, txSig, userID, amount)
    if err != nil {
        // This will trigger if the txSig is already in the database
        return fmt.Errorf("transaction already processed or database error: %w", err)
    }

    return nil
}
func (r *postgresTxRepo) IsTxProcessed(ctx context.Context, txSig string) (bool, error) {
	var exists bool
	query := "SELECT EXISTS(SELECT 1 FROM processed_transactions WHERE signature = $1)"
	err := r.db.QueryRowContext(ctx, query, txSig).Scan(&exists)
	return exists, err
}
