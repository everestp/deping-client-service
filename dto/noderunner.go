

package dto
type MessageResponse struct {
	Message string `json:"message"`
}
type RegisterRunnerRequest struct {
	OwnerPubkey string `json:"owner_pubkey"`
	NodePubkey string `json:"owner_pubkey"`
	Region      string `json:"region"`
	Latitude    string `json:"latitude"`
	Longitude   string `json:"longitude"`
}

type RunnerResponse struct {
	ID                        int     `json:"id"`
	OwnerPubkey               string  `json:"owner_pubkey"`
	NodePubkey                 string `json:"node_pubkey"`
	Region                    string  `json:"region"`
	Latitude                  float64  `json:"latitude"`
	Longitude                 float64  `json:"longitude"`
	OffchainAccumulatedTokens float64 `json:"offchain_accumulated_tokens"`
	TotalEarnedTokensAllTime  float64 `json:"total_earned_tokens_all_time"`
	PendingSolanaSync         bool    `json:"pending_solana_sync"`
}
