package controllers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/everestp/deping-client-service/config/env"
	"github.com/everestp/deping-client-service/dto"
	"github.com/everestp/deping-client-service/pkg/contextutils"
	"github.com/everestp/deping-client-service/services"
	"github.com/everestp/deping-client-service/solana"
)

type RunnerController struct {
	svc          services.NodeRunnerService
	solanaClient *solana.Client // Added to perform on-chain verification
}

func NewRunnerController(svc services.NodeRunnerService, solClient *solana.Client) *RunnerController {
	return &RunnerController{
		svc:          svc,
		solanaClient: solClient,
	}
}

// =========================
// REGISTER RUNNER
// =========================

func (c *RunnerController) Register(w http.ResponseWriter, r *http.Request) {
	var req dto.RegisterRunnerRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// ownerID := contextutils.GetUserID(r.Context())
	ownerEmail := contextutils.GetUserEmail(r.Context()) // ✅ FIXED
fmt.Printf("DEBUG: Decoded request: %+v\n", req)
fmt.Printf("DEBUG: Registering node for email: '%s'\n", ownerEmail)

    if ownerEmail == "" {
        respondError(w, http.StatusUnauthorized, "user email not found in context")
        return
    }
	resp, err := c.svc.Register(r.Context(), ownerEmail, &req)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	respondJSON(w, http.StatusCreated, resp)
}

// =========================
// GET RUNNER INFO
// =========================
type MeRequest struct {
	Pubkey string `json:"pubkey"`
}

func (c *RunnerController) Me(w http.ResponseWriter, r *http.Request) {
    var req MeRequest

    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        respondError(w, http.StatusBadRequest, "invalid request body")
        return
    }

    if req.Pubkey == "" {
        respondError(w, http.StatusBadRequest, "pubkey is required")
        return
    }

    email := contextutils.GetUserEmail(r.Context())

    // 1. Fetch the node state
     fmt.Println("Tjis is the  email and pub key ",req.Pubkey  ,email)
    node, err := c.svc.GetByEmailAndPubkey(r.Context(), req.Pubkey, email)
    fmt.Println("Tjis is the nod edos ",node)
    if err != nil {
        if errors.Is(err, sql.ErrNoRows) {
            // Nothing record present -> register
            respondJSON(w, http.StatusOK, map[string]string{"view": "register"})
            return
        }
        respondError(w, http.StatusInternalServerError, "internal server error")
        return
    }

    // 2. Determine View based on database state
    var nextView string

    switch {
    // A. Record present, node_pda is null -> activate
    case node.NodePda == "":
        nextView = "activate"

    // B. Record present, node_pda NOT null, is_validator is false -> stake
    case !node.IsValidator:
        nextView = "stake"

    // C. Record present, node_pda NOT null, is_validator is true -> dashboard
    default:
        nextView = "dashboard"
    }

    // 3. Return combined state
    respondJSON(w, http.StatusOK, map[string]interface{}{
        "view": nextView,
        "node": node,
    })
}
// =========================
// HEARTBEAT
// =========================

func (c *RunnerController) Heartbeat(w http.ResponseWriter, r *http.Request) {
	pubkey := r.URL.Query().Get("pubkey")
	if pubkey == "" {
		respondError(w, http.StatusBadRequest, "pubkey query param required")
		return
	}

	if err := c.svc.Heartbeat(r.Context(), pubkey); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, dto.MessageResponse{
		Message: "heartbeat recorded",
	})
}


func (c *RunnerController) ValidateStake(w http.ResponseWriter, r *http.Request) {
	var req dto.StakeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request")
		return
	}

	// 1. Verify On-Chain
	txInfo, err := c.solanaClient.GetTransferInfo(r.Context(), req.Signature)
	if err != nil {
		respondError(w, http.StatusBadRequest, "on-chain verification failed")
		return
	}

    fmt.Printf("This is the Revcoiver===========+++>",txInfo.Receiver)
     fmt.Printf("This is the Ampint===========+++>",txInfo.Amount)
      fmt.Printf("This is the Sender===========+++>",txInfo.Sender)

	// 2. Validate Vault and Amount
	if txInfo.Receiver != env.Get().StakeTreasuryOwnerAddr {
		respondError(w, http.StatusForbidden, "invalid destination vault")
		return
	}

	// 3. Service Orchestration
	email := contextutils.GetUserEmail(r.Context())
	err = c.svc.ValidateAndStake(r.Context(), email, req.Pubkey, req.Signature, txInfo.Amount)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "success"})
}

func (c *RunnerController) ValidateUnstake(w http.ResponseWriter, r *http.Request) {
	var req dto.StakeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request")
		return
	}

	// 1. Verify On-Chain
	txInfo, err := c.solanaClient.GetTransferInfo(r.Context(), req.Signature)
	if err != nil {
        fmt.Print("The error",err)
		respondError(w, http.StatusBadRequest, "on-chain verification failed",)
		return
	}

	// 2. Validate Source
	if txInfo.Sender != env.Get().StakeTreasuryOwnerAddr {
		respondError(w, http.StatusForbidden, "invalid unstake source")
		return
	}

	// 3. Service Orchestration
	email := contextutils.GetUserEmail(r.Context())
	err = c.svc.ValidateAndUnstake(r.Context(), email, req.Pubkey, req.Signature, txInfo.Amount)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "success"})
}

func (c *RunnerController) ActivateNode(w http.ResponseWriter, r *http.Request) {
    var req struct {
        Pubkey string `json:"public_key"`
        PDA    string `json:"node_pda"`
    }

    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        respondError(w, http.StatusBadRequest, "invalid request body")
        return
    }

    email := contextutils.GetUserEmail(r.Context())
    if email == "" {
        respondError(w, http.StatusUnauthorized, "unauthorized")
        return
    }

    // Execute service logic
    err := c.svc.ActivateNode(r.Context(), email, req.Pubkey, req.PDA)
    if err != nil {
        respondError(w, http.StatusUnprocessableEntity, err.Error())
        return
    }

    respondJSON(w, http.StatusOK, map[string]string{"status": "activated"})
}