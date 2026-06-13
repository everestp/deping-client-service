package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"

	"github.com/everestp/deping-client-service/config/env"
	"github.com/everestp/deping-client-service/db/repositories"
	"github.com/everestp/deping-client-service/dto"
	"github.com/everestp/deping-client-service/solana"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/redis/go-redis/v9"
		solgo "github.com/gagliardetto/solana-go"

)

type NodeRunnerService interface {
	Register(ctx context.Context, email string, req *dto.RegisterRunnerRequest) (*dto.RunnerResponse, error)
	GetByPubkey(ctx context.Context, pubkey string) (*dto.RunnerResponse, error)
	GetByEmailAndPubkey(ctx context.Context, pubkey , email string) (*dto.RunnerResponse, error)
	Heartbeat(ctx context.Context, pubkey string) error
	ValidateAndStake(ctx context.Context, email string, pubkey string, txSig string, amount uint64) error
    ValidateAndUnstake(ctx context.Context, email string, pubkey string, txSig string, amount uint64) error
	ActivateNode(ctx context.Context, email, pubkey, pda string) error
	CompletelyDeleteNode(ctx context.Context, email string, pubkey string) error
}

type runnerService struct {
	store       *repositories.Storage
	rdb         *redis.Client
	rabbitCh    *amqp.Channel
	cfg         *env.Config
	memRegistry *MemoryRegistry // Direct link to our real-time in-memory tracking pool
	solanaClient   *solana.Client
	logger *slog.Logger
}

// NewRunnerService matches the exact signature called by your app's main orchestration wireframe.
func NewRunnerService(
	store *repositories.Storage,
	rdb *redis.Client,
	rabbitCh *amqp.Channel,
	cfg *env.Config,
	memRegistry *MemoryRegistry,
	solanaClient *solana.Client,
	logger *slog.Logger,

) NodeRunnerService {
	return &runnerService{
		store:       store,
		rdb:         rdb,
		rabbitCh:    rabbitCh,
		cfg:         cfg,
		memRegistry: memRegistry,
		solanaClient: solanaClient,
		logger :logger,
	}
}
func (s *runnerService) ActivateNode(ctx context.Context, email, pubkey, pda string) error {
    // 1. Validate inputs
   ownerPK, err := solgo.PublicKeyFromBase58(pubkey)
    if err != nil {
        return fmt.Errorf("invalid pubkey: %w", err)
    }

    // 2. Re-derive the expected PDA using our Solana client
    expectedPDA, _, err := s.solanaClient.DeriveNodePDA(ownerPK, email)
    if err != nil {
        return fmt.Errorf("failed to derive PDA: %w", err)
    }

    // 3. Security: Check if provided PDA matches calculated PDA
    if expectedPDA.String() != pda {
        return fmt.Errorf("security alert: PDA mismatch")
    }

    // 4. Update the DB
    return s.store.NodeRuunerRepo.UpdateNodePDA(ctx, email, pubkey, pda)
}
func (s *runnerService) Register(ctx context.Context, email string, req *dto.RegisterRunnerRequest) (*dto.RunnerResponse, error) {
	if req.OwnerPubkey == "" || req.Region == "" {
		return nil, errors.New("owner_pubkey and region are required")
	}

	// 💡 Convert string inputs to float64 to safely satisfy s.store.Runners.Register
	lat, _ := strconv.ParseFloat(req.Latitude, 64)
	lng, _ := strconv.ParseFloat(req.Longitude, 64)

	node, err := s.store.NodeRuunerRepo.Register(ctx, email, req.OwnerPubkey, req.NodePubkey,req.Region ,lat, lng)
	if err != nil {
		return nil, fmt.Errorf("register runner: %w", err)
	}
	return toRunnerResponse(node), nil
}

func (s *runnerService) GetByPubkey(ctx context.Context, pubkey string) (*dto.RunnerResponse, error) {
	node, err := s.store.NodeRuunerRepo.FindByPubkey(ctx, pubkey)
	if err != nil {
		return nil, fmt.Errorf("runner not found: %w", err)
	}
	return toRunnerResponse(node), nil
}
func (s *runnerService) GetByEmailAndPubkey(ctx context.Context, pubkey, email string) (*dto.RunnerResponse, error) {
    node, err := s.store.NodeRuunerRepo.FindByEmailAndPubkey(ctx, email, pubkey)
    if err != nil {
        // Return the error directly so the controller can check errors.Is(err, sql.ErrNoRows)
        return nil, err
    }

    // Return the mapped DTO
    return toRunnerResponse(node), nil
}
func (s *runnerService) Heartbeat(ctx context.Context, pubkey string) error {
	// 1. Persist the database-backed timestamp update
	err := s.store.NodeRuunerRepo.UpdateHeartbeat(ctx, pubkey)
	if err != nil {
		return fmt.Errorf("update database heartbeat: %w", err)
	}

	// 2. Fetch node properties to extract coordinates and owner mappings
	node, err := s.store.NodeRuunerRepo.FindByPubkey(ctx, pubkey)
	if err != nil {
		return fmt.Errorf("resolve runner for memory allocation: %w", err)
	}

	// 3. Track state changes instantly inside our fast memory topology
	s.memRegistry.TrackHeartbeat(node.OwnerPubkey, node.OwnerEmail, node.Latitude, node.Longitude)

	return nil
}
// ── STAKING LIFECYCLE (REPLAY PROTECTED) ──────────────────────────────────────

func (s *runnerService) ValidateAndStake(ctx context.Context, email string, pubkey string, txSig string, amount uint64) error {
    processed, err := s.store.TxRepo.IsTxProcessed(ctx, txSig)
    if err != nil || processed {
        return errors.New("transaction already processed or invalid")
    }

    // Atomic Update: Update DB stake AND record signature in one workflow
    if err := s.store.NodeRuunerRepo.UpdateStake(ctx, pubkey, int64(amount)); err != nil {
        return fmt.Errorf("stake update failed: %w", err)
    }

    user, err := s.store.Users.GetUserByEmail(ctx,email)
    if err != nil {
        return err
    }

    return s.store.TxRepo.ProcessPayment(ctx, user.ID, amount, txSig)
}

func (s *runnerService) ValidateAndUnstake(ctx context.Context, email string, pubkey string, txSig string, amount uint64) error {
    processed, err := s.store.TxRepo.IsTxProcessed(ctx, txSig)
    if err != nil || processed {
        return errors.New("transaction already processed or invalid")
    }

    if err := s.store.NodeRuunerRepo.UpdateUnstake(ctx, pubkey, int64(amount)); err != nil {
        return fmt.Errorf("unstake update failed: %w", err)
    }

    user, err := s.store.Users.GetUserByEmail(ctx, email)
    if err != nil {
        return err
    }

    return s.store.TxRepo.ProcessPayment(ctx, user.ID, amount, txSig)
}

func (s *runnerService) CompletelyDeleteNode(ctx context.Context, email string, pubkey string) error {
	// 1. Fetch user by email to ensure the session context belongs to a valid user account
	_, err := s.store.Users.GetUserByEmail(ctx, email)
	if err != nil {
		return fmt.Errorf("user authentication lookup failed: %w", err)
	}

	// 2. Execute the hard delete in your repository layer using the node_pda/pubkey
	if err := s.store.NodeRuunerRepo.DeleteNode(ctx, pubkey); err != nil {
		return fmt.Errorf("node permanent deletion failed for pubkey %s: %w", pubkey, err)
	}

	return nil
}
// ── HELPERS ──────────────────────────────────────────────────────────────────

func toRunnerResponse(n *repositories.RunnerNode) *dto.RunnerResponse {
    return &dto.RunnerResponse{
        ID:                        n.ID,
        OwnerPubkey:               n.OwnerPubkey,
        OwnerEmail:                n.OwnerEmail,
        Region:                    n.Region,
        NodePubkey:                n.NodePubkey,
        Latitude:                  n.Latitude,
        Longitude:                 n.Longitude,
        OffchainAccumulatedTokens: n.OffchainAccumulatedTokens,
        TotalEarnedTokensAllTime:  n.TotalEarnedTokensAllTime,
        PendingSolanaSync:         n.PendingSolanaSync,
        NodePda:                   n.NodePda.String,
        IsValidator:               n.IsValidator,
    }
}