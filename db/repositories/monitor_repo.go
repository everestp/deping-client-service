package repositories

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

type Monitor struct {
	ID                   string
	OwnerID              int
	TargetURL            string
	CheckIntervalSeconds int
	CreditBalanceChecks  int64
	TotalSpentTokens     float64
	IsActive             bool
	CreatedAt            time.Time
}

type MonitorRepository interface {
	Create(ctx context.Context, ownerID int, targetURL string, intervalSeconds int) (*Monitor, error)
	FindByOwner(ctx context.Context, ownerID int) ([]*Monitor, error)
	FindActive(ctx context.Context) ([]*Monitor, error)
	FindMany(ctx context.Context, ids []string) ([]*Monitor, error)
	UpdateActive(ctx context.Context, id string, isActive bool) error
	DeductCredit(ctx context.Context, id string, tokenCost float64) error
	Delete(ctx context.Context, id string, ownerID int) error
	FindByJobID(ctx context.Context, jobID string) (*Monitor, error)
	FindByID(ctx context.Context, id string) (*Monitor, error)
}
type postgresMonitorRepo struct {
    db *sql.DB
}

func NewMonitorRepository(db *sql.DB) MonitorRepository {
    return &postgresMonitorRepo{db: db}
}

func (r *postgresMonitorRepo) Create(ctx context.Context, ownerID int, targetURL string, intervalSeconds int) (*Monitor, error) {
    const q = `
        INSERT INTO monitors (owner_id, target_url, check_interval_seconds)
        VALUES ($1, $2, $3)
        RETURNING id, owner_id, target_url, check_interval_seconds, total_spent_tokens, is_active, created_at`

    m := &Monitor{}
    err := r.db.QueryRowContext(ctx, q, ownerID, targetURL, intervalSeconds).
        Scan(&m.ID, &m.OwnerID, &m.TargetURL, &m.CheckIntervalSeconds, &m.TotalSpentTokens, &m.IsActive, &m.CreatedAt)
    return m, err
}

func (r *postgresMonitorRepo) FindByOwner(ctx context.Context, ownerID int) ([]*Monitor, error) {
    const q = `SELECT id, owner_id, target_url, check_interval_seconds, total_spent_tokens, is_active, created_at 
               FROM monitors WHERE owner_id = $1 AND deleted_at IS NULL ORDER BY created_at DESC`
    return r.scanMany(ctx, q, ownerID)
}

func (r *postgresMonitorRepo) FindActive(ctx context.Context) ([]*Monitor, error) {
    // Joins with users to verify global credit balance
    const q = `SELECT m.id, m.owner_id, m.target_url, m.check_interval_seconds, m.total_spent_tokens, m.is_active, m.created_at
               FROM monitors m JOIN users u ON m.owner_id = u.id
               WHERE m.is_active = TRUE AND m.deleted_at IS NULL AND u.credit_balance > 0`
    return r.scanMany(ctx, q)
}

func (r *postgresMonitorRepo) FindMany(ctx context.Context, ids []string) ([]*Monitor, error) {
    if len(ids) == 0 { return nil, nil }
    placeholders, args := make([]string, len(ids)), make([]interface{}, len(ids))
    for i, id := range ids {
        placeholders[i] = fmt.Sprintf("$%d", i+1)
        args[i] = id
    }
    q := fmt.Sprintf(`SELECT m.id, m.owner_id, m.target_url, m.check_interval_seconds, m.total_spent_tokens, m.is_active, m.created_at 
                      FROM monitors m JOIN users u ON m.owner_id = u.id
                      WHERE m.id IN (%s) AND m.is_active = TRUE AND m.deleted_at IS NULL AND u.credit_balance > 0`, strings.Join(placeholders, ","))
    return r.scanMany(ctx, q, args...)
}

func (r *postgresMonitorRepo) FindByID(ctx context.Context, id string) (*Monitor, error) {
    const q = `SELECT id, owner_id, target_url, check_interval_seconds, total_spent_tokens, is_active, created_at 
               FROM monitors WHERE id = $1 AND deleted_at IS NULL`
    m := &Monitor{}
    err := r.db.QueryRowContext(ctx, q, id).
        Scan(&m.ID, &m.OwnerID, &m.TargetURL, &m.CheckIntervalSeconds, &m.TotalSpentTokens, &m.IsActive, &m.CreatedAt)
    return m, err
}

func (r *postgresMonitorRepo) DeductCredit(ctx context.Context, id string, tokenCost float64) error {
    // Logic moved to atomic ProcessJobSettlement; this only tracks usage.
    _, err := r.db.ExecContext(ctx, `UPDATE monitors SET total_spent_tokens = total_spent_tokens + $1 WHERE id = $2`, tokenCost, id)
    return err
}

// Helper to keep code dry
func (r *postgresMonitorRepo) scanMany(ctx context.Context, q string, args ...interface{}) ([]*Monitor, error) {
    rows, err := r.db.QueryContext(ctx, q, args...)
    if err != nil { return nil, err }
    defer rows.Close()
    var result []*Monitor
    for rows.Next() {
        m := &Monitor{}
        if err := rows.Scan(&m.ID, &m.OwnerID, &m.TargetURL, &m.CheckIntervalSeconds, &m.TotalSpentTokens, &m.IsActive, &m.CreatedAt); err != nil {
            return nil, err
        }
        result = append(result, m)
    }
    return result, rows.Err()
}

func (r *postgresMonitorRepo) FindByJobID(ctx context.Context, jobID string) (*Monitor, error) {
    parts := strings.Split(jobID, ":")
    if len(parts) == 0 || parts[0] == "" {
        return nil, fmt.Errorf("malformed job_id: %q", jobID)
    }
    // Now simply calls the updated FindByID
    return r.FindByID(ctx, parts[0])
}

func (r *postgresMonitorRepo) UpdateActive(ctx context.Context, id string, isActive bool) error {
    const q = `UPDATE monitors SET is_active = $1 WHERE id = $2 AND deleted_at IS NULL`
    _, err := r.db.ExecContext(ctx, q, isActive, id)
    return err
}

func (r *postgresMonitorRepo) Delete(ctx context.Context, id string, ownerID int) error {
    const q = `UPDATE monitors SET deleted_at = NOW() WHERE id = $1 AND owner_id = $2`
    _, err := r.db.ExecContext(ctx, q, id, ownerID)
    return err
}