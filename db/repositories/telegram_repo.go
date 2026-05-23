package repositories

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/everestp/deping-client-service/dto"
	_ "github.com/lib/pq"
)

// ═══════════════════════════════════════════════════════════════════════════
// Domain Models (repository layer)
// ═══════════════════════════════════════════════════════════════════════════

type TelegramUser struct {
	ID               int
	UserID           int
	TelegramChatID   sql.NullInt64
	TelegramUsername sql.NullString
	VerificationCode sql.NullString
	IsVerified       bool
	LastRemindedAt   sql.NullTime
	CreatedAt        time.Time
}

type TelegramCredit struct {
	UserID           int
	FreeCreditsUsed  int
	FreeResetDate    time.Time
	PurchasedCredits int
}

type MonitorHealthState struct {
	MonitorID          string
	IsDown             bool
	ConsecutiveFailures int
	AlertSent          bool
	LastAlertedAt      sql.NullTime
}

type TelegramSubscription struct {
	ID                     int
	UserID                 int
	MonitorID              string
	IsNotificationsEnabled bool
}

// ═══════════════════════════════════════════════════════════════════════════
// Repository Interfaces — depend on abstractions, not concretions
// ═══════════════════════════════════════════════════════════════════════════

// TelegramRepository covers all Telegram-related persistence.
type TelegramRepository interface {
	// Account linking
	UpsertTelegramUser(ctx context.Context, userID int, username string, code string) error
	GetTelegramUserByUserID(ctx context.Context, userID int) (*TelegramUser, error)
	GetTelegramUserByCode(ctx context.Context, code string) (*TelegramUser, error)
	GetTelegramUserByChatID(ctx context.Context, chatID int64) (*TelegramUser, error)
	VerifyTelegramUser(ctx context.Context, userID int, chatID int64, username string) error

	// Credits
	GetCreditStatus(ctx context.Context, userID int) (*TelegramCredit, error)
	DeductCredit(ctx context.Context, userID int) (success bool, creditType string, err error)
	AddPurchasedCredits(ctx context.Context, userID int, amount int, txSignature string) (newBalance int, err error)
	ShouldSendReminder(ctx context.Context, userID int) (bool, error)
	MarkReminderSent(ctx context.Context, userID int) error

	// Health state (Two-Packet rule)
	GetHealthState(ctx context.Context, monitorID string) (*MonitorHealthState, error)
	UpsertHealthState(ctx context.Context, state *MonitorHealthState) error

	// Subscriptions
	GetSubscription(ctx context.Context, userID int, monitorID string) (*TelegramSubscription, error)
	UpsertSubscription(ctx context.Context, userID int, monitorID string, enabled bool) error
	GetMonitorOwnerChatID(ctx context.Context, monitorID string) (chatID int64, userID int, err error)
	IsNotificationEnabled(ctx context.Context, monitorID string) (bool, error)

	// Bot queries
	GetUserMonitorSummaries(ctx context.Context, chatID int64) ([]dto.MonitorSummary, error)
	GetRecentPings(ctx context.Context, monitorID string, limit int) ([]dto.RecentPingRow, error)
	GetMonitorByOwnerAndIndex(ctx context.Context, userID int, index int) (string /*monitorID*/, string /*url*/, error)
}

// ═══════════════════════════════════════════════════════════════════════════
// PostgreSQL Implementation
// ═══════════════════════════════════════════════════════════════════════════

type postgressTelegramRepo struct {
	db *sql.DB
}

func NewTelegramRepository(db *sql.DB) TelegramRepository {
	return &postgressTelegramRepo{db: db}
}

// ── Account Linking ───────────────────────────────────────────────────────

func (r *postgressTelegramRepo) UpsertTelegramUser(ctx context.Context, userID int, username string, code string) error {
    query := `
        INSERT INTO telegram_users (user_id, telegram_username, verification_code, is_verified)
        VALUES ($1, $2, $3, FALSE)
        ON CONFLICT (user_id) DO UPDATE SET
            telegram_username = EXCLUDED.telegram_username,
            verification_code = EXCLUDED.verification_code,
            is_verified = FALSE; -- Reset verification if they are re-linking
    `
    _, err := r.db.ExecContext(ctx, query, userID, username, code)
    return err
}

func (r *postgressTelegramRepo) GetTelegramUserByUserID(ctx context.Context, userID int) (*TelegramUser, error) {
	const q = `
		SELECT id, user_id, telegram_chat_id, telegram_username, verification_code,
		       is_verified, last_reminded_at, created_at
		FROM telegram_users
		WHERE user_id = $1 AND deleted_at IS NULL`
	return r.scanTelegramUser(r.db.QueryRowContext(ctx, q, userID))
}

func (r *postgressTelegramRepo) GetTelegramUserByCode(ctx context.Context, code string) (*TelegramUser, error) {
	const q = `
		SELECT id, user_id, telegram_chat_id, telegram_username, verification_code,
		       is_verified, last_reminded_at, created_at
		FROM telegram_users
		WHERE verification_code = $1 AND is_verified = FALSE AND deleted_at IS NULL`
	return r.scanTelegramUser(r.db.QueryRowContext(ctx, q, code))
}

func (r *postgressTelegramRepo) GetTelegramUserByChatID(ctx context.Context, chatID int64) (*TelegramUser, error) {
	const q = `
		SELECT id, user_id, telegram_chat_id, telegram_username, verification_code,
		       is_verified, last_reminded_at, created_at
		FROM telegram_users
		WHERE telegram_chat_id = $1 AND deleted_at IS NULL`
	return r.scanTelegramUser(r.db.QueryRowContext(ctx, q, chatID))
}

func (r *postgressTelegramRepo) VerifyTelegramUser(ctx context.Context, userID int, chatID int64, username string) error {
	const q = `
		UPDATE telegram_users
		SET telegram_chat_id    = $2,
		    telegram_username   = $3,
		    verification_code   = NULL,
		    is_verified         = TRUE,
		    updated_at          = NOW()
		WHERE user_id = $1 AND deleted_at IS NULL`
	res, err := r.db.ExecContext(ctx, q, userID, chatID, username)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("telegram_user not found for user_id=%d", userID)
	}
	return nil
}

func (r *postgressTelegramRepo) scanTelegramUser(row *sql.Row) (*TelegramUser, error) {
	u := &TelegramUser{}
	err := row.Scan(
		&u.ID, &u.UserID, &u.TelegramChatID, &u.TelegramUsername,
		&u.VerificationCode, &u.IsVerified, &u.LastRemindedAt, &u.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return u, err
}

// ── Credits ───────────────────────────────────────────────────────────────

func (r *postgressTelegramRepo) GetCreditStatus(ctx context.Context, userID int) (*TelegramCredit, error) {
	// Ensure row exists
	const upsert = `
		INSERT INTO telegram_credits (user_id) VALUES ($1)
		ON CONFLICT (user_id) DO NOTHING`
	_, _ = r.db.ExecContext(ctx, upsert, userID)

	const q = `
		SELECT user_id, free_credits_used, free_reset_date, purchased_credits
		FROM telegram_credits WHERE user_id = $1`
	c := &TelegramCredit{}
	err := r.db.QueryRowContext(ctx, q, userID).Scan(
		&c.UserID, &c.FreeCreditsUsed, &c.FreeResetDate, &c.PurchasedCredits,
	)
	return c, err
}

func (r *postgressTelegramRepo) DeductCredit(ctx context.Context, userID int) (bool, string, error) {
	const q = `SELECT success, credit_type FROM deduct_telegram_credit($1)`
	var success bool
	var creditType string
	err := r.db.QueryRowContext(ctx, q, userID).Scan(&success, &creditType)
	return success, creditType, err
}

func (r *postgressTelegramRepo) AddPurchasedCredits(ctx context.Context, userID int, amount int, txSignature string) (int, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	var newBalance int
	const q = `SELECT add_purchased_credits($1, $2)`
	if err := tx.QueryRowContext(ctx, q, userID, amount).Scan(&newBalance); err != nil {
		return 0, err
	}
	// Audit: store tx signature in solana_sync_events reuse pattern
	const audit = `
		INSERT INTO solana_sync_events (runner_pubkey, tx_signature, amount_raw)
		VALUES ('telegram_credit:' || $1::TEXT, $2, $3)`
	if _, err := tx.ExecContext(ctx, audit, userID, txSignature, amount); err != nil {
		return 0, err
	}
	return newBalance, tx.Commit()
}

func (r *postgressTelegramRepo) ShouldSendReminder(ctx context.Context, userID int) (bool, error) {
	const q = `
		SELECT last_reminded_at IS NULL OR last_reminded_at < NOW() - INTERVAL '7 days'
		FROM telegram_users WHERE user_id = $1 AND deleted_at IS NULL`
	var should bool
	err := r.db.QueryRowContext(ctx, q, userID).Scan(&should)
	return should, err
}

func (r *postgressTelegramRepo) MarkReminderSent(ctx context.Context, userID int) error {
	const q = `
		UPDATE telegram_users SET last_reminded_at = NOW()
		WHERE user_id = $1 AND deleted_at IS NULL`
	_, err := r.db.ExecContext(ctx, q, userID)
	return err
}

// ── Health State ──────────────────────────────────────────────────────────

func (r *postgressTelegramRepo) GetHealthState(ctx context.Context, monitorID string) (*MonitorHealthState, error) {
	const q = `
		SELECT monitor_id, is_down, consecutive_failures, alert_sent, last_alerted_at
		FROM monitor_health_state WHERE monitor_id = $1`
	s := &MonitorHealthState{}
	err := r.db.QueryRowContext(ctx, q, monitorID).Scan(
		&s.MonitorID, &s.IsDown, &s.ConsecutiveFailures, &s.AlertSent, &s.LastAlertedAt,
	)
	if err == sql.ErrNoRows {
		// Return zero-state for new monitors
		return &MonitorHealthState{MonitorID: monitorID}, nil
	}
	return s, err
}

func (r *postgressTelegramRepo) UpsertHealthState(ctx context.Context, s *MonitorHealthState) error {
	const q = `
		INSERT INTO monitor_health_state
			(monitor_id, is_down, consecutive_failures, alert_sent, last_alerted_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (monitor_id) DO UPDATE
			SET is_down              = EXCLUDED.is_down,
			    consecutive_failures = EXCLUDED.consecutive_failures,
			    alert_sent           = EXCLUDED.alert_sent,
			    last_alerted_at      = EXCLUDED.last_alerted_at,
			    updated_at           = NOW()`
	var lastAleratedAt interface{}
	if s.LastAlertedAt.Valid {
		lastAleratedAt = s.LastAlertedAt.Time
	}
	_, err := r.db.ExecContext(ctx, q,
		s.MonitorID, s.IsDown, s.ConsecutiveFailures, s.AlertSent, lastAleratedAt,
	)
	return err
}

// ── Subscriptions ─────────────────────────────────────────────────────────

func (r *postgressTelegramRepo) GetSubscription(ctx context.Context, userID int, monitorID string) (*TelegramSubscription, error) {
	const q = `
		SELECT id, user_id, monitor_id, is_notifications_enabled
		FROM telegram_subscriptions WHERE user_id = $1 AND monitor_id = $2`
	s := &TelegramSubscription{}
	err := r.db.QueryRowContext(ctx, q, userID, monitorID).Scan(
		&s.ID, &s.UserID, &s.MonitorID, &s.IsNotificationsEnabled,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return s, err
}

func (r *postgressTelegramRepo) UpsertSubscription(ctx context.Context, userID int, monitorID string, enabled bool) error {
	const q = `
		INSERT INTO telegram_subscriptions (user_id, monitor_id, is_notifications_enabled)
		VALUES ($1, $2, $3)
		ON CONFLICT (user_id, monitor_id) DO UPDATE
			SET is_notifications_enabled = EXCLUDED.is_notifications_enabled,
			    updated_at               = NOW()`
	_, err := r.db.ExecContext(ctx, q, userID, monitorID, enabled)
	return err
}

// GetMonitorOwnerChatID returns the Telegram chat ID for the owner of a monitor.
// Used by the alert service to know who to notify.
func (r *postgressTelegramRepo) GetMonitorOwnerChatID(ctx context.Context, monitorID string) (int64, int, error) {
	const q = `
		SELECT tu.telegram_chat_id, m.owner_id
		FROM monitors m
		JOIN telegram_users tu ON tu.user_id = m.owner_id
		WHERE m.id = $1
		  AND m.deleted_at IS NULL
		  AND tu.is_verified = TRUE
		  AND tu.deleted_at IS NULL`
	var chatID int64
	var userID int
	err := r.db.QueryRowContext(ctx, q, monitorID).Scan(&chatID, &userID)
	if err == sql.ErrNoRows {
		return 0, 0, nil
	}
	return chatID, userID, err
}

func (r *postgressTelegramRepo) IsNotificationEnabled(ctx context.Context, monitorID string) (bool, error) {
	const q = `
		SELECT COALESCE(
			(SELECT is_notifications_enabled
			 FROM telegram_subscriptions ts
			 JOIN monitors m ON m.owner_id = ts.user_id
			 WHERE ts.monitor_id = $1 AND m.id = $1
			 LIMIT 1),
			TRUE  -- default: enabled if no subscription row yet
		)`
	var enabled bool
	err := r.db.QueryRowContext(ctx, q, monitorID).Scan(&enabled)
	return enabled, err
}

// ── Bot Queries ───────────────────────────────────────────────────────────

func (r *postgressTelegramRepo) GetUserMonitorSummaries(ctx context.Context, chatID int64) ([]dto.MonitorSummary, error) {
	const q = `
		SELECT
			m.id,
			m.target_url,
			m.is_active,
			COALESCE(hs.is_down, FALSE)         AS is_down,
			COALESCE(
				(SELECT ROUND(100.0 * COUNT(*) FILTER (WHERE success) / NULLIF(COUNT(*),0), 2)
				 FROM ping_logs pl
				 WHERE pl.monitor_id = m.id
				   AND pl.timestamp  >= NOW() - INTERVAL '24 hours'),
				100.0
			)                                   AS uptime_pct_24h,
			COALESCE(
				(SELECT AVG(latency_ms)
				 FROM ping_logs pl
				 WHERE pl.monitor_id = m.id
				   AND pl.timestamp  >= NOW() - INTERVAL '24 hours'),
				0.0
			)                                   AS avg_latency_ms
		FROM monitors m
		JOIN telegram_users tu ON tu.user_id = m.owner_id
		LEFT JOIN monitor_health_state hs ON hs.monitor_id = m.id
		WHERE tu.telegram_chat_id = $1
		  AND m.deleted_at         IS NULL
		  AND tu.deleted_at        IS NULL
		  AND tu.is_verified       = TRUE
		ORDER BY m.created_at DESC`

	rows, err := r.db.QueryContext(ctx, q, chatID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var summaries []dto.MonitorSummary
	for rows.Next() {
		var s dto.MonitorSummary
		if err := rows.Scan(&s.ID, &s.TargetURL, &s.IsActive, &s.IsDown,
			&s.UptimePct, &s.AvgLatency); err != nil {
			return nil, err
		}
		summaries = append(summaries, s)
	}
	return summaries, rows.Err()
}

func (r *postgressTelegramRepo) GetRecentPings(ctx context.Context, monitorID string, limit int) ([]dto.RecentPingRow, error) {
	const q = `
		SELECT timestamp, success, latency_ms, status_code, geo_region,
		       error_kind, dns_us, tcp_us, tls_us, ttfb_us, total_us
		FROM ping_logs
		WHERE monitor_id = $1
		ORDER BY timestamp DESC
		LIMIT $2`
	rows, err := r.db.QueryContext(ctx, q, monitorID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var pings []dto.RecentPingRow
	for rows.Next() {
		var p dto.RecentPingRow
		if err := rows.Scan(&p.Timestamp, &p.Success, &p.LatencyMs, &p.StatusCode,
			&p.GeoRegion, &p.ErrorKind, &p.DnsUs, &p.TcpUs, &p.TlsUs,
			&p.TtfbUs, &p.TotalUs); err != nil {
			return nil, err
		}
		pings = append(pings, p)
	}
	return pings, rows.Err()
}

func (r *postgressTelegramRepo) GetMonitorByOwnerAndIndex(ctx context.Context, userID int, index int) (string, string, error) {
	const q = `
		SELECT id, target_url
		FROM monitors
		WHERE owner_id = $1 AND deleted_at IS NULL
		ORDER BY created_at DESC
		LIMIT 1 OFFSET $2`
	var id, url string
	err := r.db.QueryRowContext(ctx, q, userID, index).Scan(&id, &url)
	if err == sql.ErrNoRows {
		return "", "", nil
	}
	return id, url, err
}
