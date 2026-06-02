package controllers

import (
    "encoding/json"

    "net/http"

    // 1. Rename external library to 'solgo' to avoid collision


    // 2. Import your internal client package
    "github.com/everestp/deping-client-service/solana"
)

type TransactionController struct {
    solanaClient *solana.Client
}

func NewTransactionController(sc *solana.Client) *TransactionController {
    return &TransactionController{solanaClient: sc}
}

type ValidateRequest struct {
    Signature      string `json:"signature"`
    ExpectedAmount uint64 `json:"expected_amount"`
    NodePDA        string `json:"node_pda"`
}

func (c *TransactionController) ValidateTransaction(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "application/json")
    var req ValidateRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, `{"error":"invalid request"}`, http.StatusBadRequest)
        return
    }


    // Fetch transaction info without validating the amount
    info, err := c.solanaClient.GetTransferInfo(r.Context(), req.Signature)
    if err != nil {
        json.NewEncoder(w).Encode(map[string]interface{}{
            "success": false,
            "error":   err.Error(),
        })
        return
    }

    // Return the logged info
    json.NewEncoder(w).Encode(map[string]interface{}{
        "success":   true,
        "amount":    info.Amount,
        "receiver":  info.Receiver,
        "timestamp": info.Timestamp,
        "message":   "transaction logged successfully",
    })
}
