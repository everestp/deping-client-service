package repositories

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type RunnerNode struct {
    ID                           int          `json:"id"`
    OwnerEmail                   string          `json:"owner_email"`
    OwnerPubkey                  string          `json:"owner_pubkey"`
    NodePubkey                   string          `json:"node_pubkey"`
    Region                       string          `json:"region"`
    Latitude                     float64         `json:"latitude"`
    Longitude                    float64         `json:"longitude"`
    OffchainAccumulatedTokens    float64         `json:"offchain_accumulated_tokens"`
    TotalEarnedTokensAllTime     float64         `json:"total_earned_tokens_all_time"`
    PendingSolanaSync            bool            `json:"pending_solana_sync"`
    LastSeenTimestamp            time.Time       `json:"last_seen_timestamp"`
    CreatedAt                    time.Time       `json:"created_at"`
    UpdatedAt                    time.Time       `json:"updated_at"`
    DeletedAt                    *time.Time      `json:"deleted_at"`
    IsValidator                  bool            `json:"is_validator"`
    StakedAmount                 string          `json:"staked_amount"`
    UnstakeRequestAt             *time.Time      `json:"unstake_request_at"`
    NodePda                      sql.NullString  `json:"node_pda"` // Correct for NULL support
}

type AccumulateResult struct {
	NewBalance float64 `json:"new_balance"`
	DidSync    bool    `json:"did_sync"`
}

type NodeRunnerRepository interface {
	Register(ctx context.Context, ownerEmail, ownerPubkey, nodePubkey, region string, lat, lng float64) (*RunnerNode, error)
	FindByPubkey(ctx context.Context, pubkey string) (*RunnerNode, error)
	FindByEmailAndPubkey(ctx context.Context, email, pubkey string) (*RunnerNode, error)
	UpdateHeartbeat(ctx context.Context, pubkey string) error
	AccumulateReward(ctx context.Context, pubkey string, delta, threshold float64) (*AccumulateResult, error)
	SetPendingSync(ctx context.Context, pubkey string, pending bool) error
}

type nodeRunnerRepo struct {
	db *sql.DB
}

func NewNodeRunnerRepository(db *sql.DB) NodeRunnerRepository {
	return &nodeRunnerRepo{
		db: db,
	}
}

// =========================================
// REGISTER
// =========================================

func (r *nodeRunnerRepo) Register(
    ctx context.Context,
    ownerEmail,
    ownerPubkey,
    nodePubkey,
    region string,
    lat,
    lng float64,
) (*RunnerNode, error) {

    const q = `
        INSERT INTO runner_nodes (
            owner_email,
            owner_pubkey,
            node_pubkey,
            region,
            latitude,
            longitude
        )
        VALUES ($1, $2, $3, $4, $5, $6)
        ON CONFLICT (node_pubkey)
        DO UPDATE SET
            owner_pubkey = EXCLUDED.owner_pubkey,
            region = EXCLUDED.region,
            latitude = EXCLUDED.latitude,
            longitude = EXCLUDED.longitude,
            last_seen_timestamp = NOW(),
            updated_at = NOW()
        RETURNING
            id,
            owner_email,
            owner_pubkey,
            node_pubkey,
            region,
            latitude,
            longitude,
            offchain_accumulated_tokens,
            total_earned_tokens_all_time,
            pending_solana_sync,
            last_seen_timestamp,
            created_at,
            updated_at
    `

    n := &RunnerNode{}

    err := r.db.QueryRowContext(
        ctx,
        q,
        ownerEmail,
        ownerPubkey,
        nodePubkey,
        region,
        lat,
        lng,
    ).Scan(
        &n.ID,
        &n.OwnerEmail,
        &n.OwnerPubkey,
        &n.NodePubkey,
        &n.Region,
        &n.Latitude,
        &n.Longitude,
        &n.OffchainAccumulatedTokens,
        &n.TotalEarnedTokensAllTime,
        &n.PendingSolanaSync,
        &n.LastSeenTimestamp,
        &n.CreatedAt,
        &n.UpdatedAt,
    )

    if err != nil {
        return nil, fmt.Errorf("nodeRunnerRepo.Register: %w", err)
    }

    return n, nil
}

// =========================================
// FIND BY PUBKEY
// =========================================

func (r *nodeRunnerRepo) FindByPubkey(
	ctx context.Context,
	pubkey string,
) (*RunnerNode, error) {

	const q = `
		SELECT
			id,
			owner_email,
			owner_pubkey,
			node_pubkey,
			region,
			latitude,
			longitude,
			offchain_accumulated_tokens,
			total_earned_tokens_all_time,
			pending_solana_sync,
			last_seen_timestamp,
			created_at,
			updated_at
		FROM runner_nodes
		WHERE owner_pubkey = $1
		  AND deleted_at IS NULL
	`

	n := &RunnerNode{}

	err := r.db.QueryRowContext(ctx, q, pubkey).Scan(
		&n.ID,
		&n.OwnerEmail,
		&n.OwnerPubkey,
		&n.NodePubkey,
		&n.Region,
		&n.Latitude,
		&n.Longitude,
		&n.OffchainAccumulatedTokens,
		&n.TotalEarnedTokensAllTime,
		&n.PendingSolanaSync,
		&n.LastSeenTimestamp,
		&n.CreatedAt,
		&n.UpdatedAt,
	)

	if err != nil {
		return nil, fmt.Errorf("nodeRunnerRepo.FindByPubkey: %w", err)
	}

	return n, nil
}

// =========================================
// FIND BY EMAIL + PUBKEY
// =========================================

func (r *nodeRunnerRepo) FindByEmailAndPubkey(
    ctx context.Context,
    email string,
    pubkey string,
) (*RunnerNode, error) {

    const q = `
        SELECT
            id,
            owner_email,
            owner_pubkey,
            node_pubkey,
            region,
            latitude,
            longitude,
            offchain_accumulated_tokens,
            total_earned_tokens_all_time,
            pending_solana_sync,
            last_seen_timestamp,
            created_at,
            updated_at,
            deleted_at,
            is_validator,
            staked_amount,
            unstake_request_at,
            node_pda
        FROM runner_nodes
        WHERE owner_email = $1
          AND owner_pubkey = $2
          AND deleted_at IS NULL
        ORDER BY created_at DESC
        LIMIT 1
    `

    var n RunnerNode

    err := r.db.QueryRowContext(ctx, q, email, pubkey).Scan(
        &n.ID,
        &n.OwnerEmail,
        &n.OwnerPubkey,
        &n.NodePubkey,
        &n.Region,
        &n.Latitude,
        &n.Longitude,
        &n.OffchainAccumulatedTokens,
        &n.TotalEarnedTokensAllTime,
        &n.PendingSolanaSync,
        &n.LastSeenTimestamp,
        &n.CreatedAt,
        &n.UpdatedAt,
        &n.DeletedAt,
        &n.IsValidator,
        &n.StakedAmount,
        &n.UnstakeRequestAt,
        &n.NodePda,
    )
if err != nil {
        if errors.Is(err, sql.ErrNoRows) {
            return nil, sql.ErrNoRows // Return raw error for controller to detect
        }
        return nil, fmt.Errorf("db error: %w", err)
    }
    return &n, nil
}

// =========================================
// UPDATE HEARTBEAT
// =========================================

func (r *nodeRunnerRepo) UpdateHeartbeat(
	ctx context.Context,
	pubkey string,
) error {

	const q = `
		UPDATE runner_nodes
		SET last_seen_timestamp = NOW()
		WHERE owner_pubkey = $1
	`

	_, err := r.db.ExecContext(ctx, q, pubkey)
	if err != nil {
		return fmt.Errorf("nodeRunnerRepo.UpdateHeartbeat: %w", err)
	}

	return nil
}

// =========================================
// ACCUMULATE REWARD
// =========================================

func (r *nodeRunnerRepo) AccumulateReward(
	ctx context.Context,
	pubkey string,
	delta,
	threshold float64,
) (*AccumulateResult, error) {

	const q = `
		SELECT new_balance, did_sync
		FROM accumulate_runner_reward($1, $2, $3)
	`

	res := &AccumulateResult{}

	err := r.db.QueryRowContext(
		ctx,
		q,
		pubkey,
		delta,
		threshold,
	).Scan(
		&res.NewBalance,
		&res.DidSync,
	)

	if err != nil {
		return nil, fmt.Errorf("nodeRunnerRepo.AccumulateReward: %w", err)
	}

	return res, nil
}

// =========================================
// SET PENDING SYNC
// =========================================

func (r *nodeRunnerRepo) SetPendingSync(
	ctx context.Context,
	pubkey string,
	pending bool,
) error {

	const q = `
		UPDATE runner_nodes
		SET pending_solana_sync = $1
		WHERE owner_pubkey = $2
	`

	_, err := r.db.ExecContext(ctx, q, pending, pubkey)
	if err != nil {
		return fmt.Errorf("nodeRunnerRepo.SetPendingSync: %w", err)
	}

	return nil
}
