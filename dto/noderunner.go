

package dto
type MessageResponse struct {
	Message string `json:"message"`
}
type RegisterRunnerRequest struct {
	OwnerPubkey string `json:"owner_pubkey"`
	NodePubkey string `json:"node_pubkey"`
	Region      string `json:"region"`
	Latitude    string `json:"latitude"`
	Longitude   string `json:"longitude"`
}

type RunnerResponse struct {
    ID                        int     `json:"id"`
    OwnerEmail                string  `json:"owner_email"`
    OwnerPubkey               string  `json:"owner_pubkey"`
    NodePubkey                string  `json:"node_pubkey"`
    Region                    string  `json:"region"`
    Latitude                  float64 `json:"latitude"`
    Longitude                 float64 `json:"longitude"`
    OffchainAccumulatedTokens float64 `json:"offchain_accumulated_tokens"`
    TotalEarnedTokensAllTime  float64 `json:"total_earned_tokens_all_time"`
    PendingSolanaSync         bool    `json:"pending_solana_sync"`

    // State management fields for the frontend
    IsValidator      bool    `json:"is_validator"`
    StakedAmount     float64 `json:"staked_amount"`
    NodePda          string  `json:"node_pda"` // Use string to easily check if empty in JSON
    UnstakeRequestAt *string `json:"unstake_request_at"`
}




type StakeRequest struct {
    Signature string `json:"signature"`
    Amount    uint64 `json:"expected_amount"` // Raw integer amount (lamports)
    NodePda  string  `json:"node_pda"`
    Pubkey    string `json:"public_key"`
}

type DeleteNodeRequest struct {
  
    NodePda  string  `json:"node_pda"`
    Pubkey    string `json:"public_key"`
}
type TransactionStatusResponse struct {
    Status  string `json:"status"`
    Message string `json:"message"`
}
