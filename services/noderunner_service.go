package services

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/everestp/deping-client-service/config/env"
	"github.com/everestp/deping-client-service/db/repositories"
	"github.com/everestp/deping-client-service/dto"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/redis/go-redis/v9"
)

type NodeRunnerService interface {
	Register(ctx context.Context, email string, req *dto.RegisterRunnerRequest) (*dto.RunnerResponse, error)
	GetByPubkey(ctx context.Context, pubkey string) (*dto.RunnerResponse, error)
	GetByEmailAndPubkey(ctx context.Context, pubkey , email string) (*dto.RunnerResponse, error)
	Heartbeat(ctx context.Context, pubkey string) error
}

type runnerService struct {
	store       *repositories.Storage
	rdb         *redis.Client
	rabbitCh    *amqp.Channel
	cfg         *env.Config
	memRegistry *MemoryRegistry // Direct link to our real-time in-memory tracking pool
}

// NewRunnerService matches the exact signature called by your app's main orchestration wireframe.
func NewRunnerService(
	store *repositories.Storage,
	rdb *redis.Client,
	rabbitCh *amqp.Channel,
	cfg *env.Config,
	memRegistry *MemoryRegistry,
) NodeRunnerService {
	return &runnerService{
		store:       store,
		rdb:         rdb,
		rabbitCh:    rabbitCh,
		cfg:         cfg,
		memRegistry: memRegistry,
	}
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

func toRunnerResponse(n *repositories.RunnerNode) *dto.RunnerResponse {
	return &dto.RunnerResponse{
		ID:                        n.ID,
		OwnerPubkey:               n.OwnerPubkey,
		OwnerEmail:     n.OwnerEmail,
		Region:                    n.Region,
		NodePubkey:                n.NodePubkey,

		Latitude:                   n.Latitude,
		Longitude:                 n.Longitude,
		OffchainAccumulatedTokens: n.OffchainAccumulatedTokens,
		TotalEarnedTokensAllTime:  n.TotalEarnedTokensAllTime,
		PendingSolanaSync:         n.PendingSolanaSync,
		NodePda:   n.NodePda.String,
		IsValidator: n.IsValidator,
	}
}
