package controllers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/everestp/deping-client-service/dto"
	"github.com/everestp/deping-client-service/pkg/contextutils"
	"github.com/everestp/deping-client-service/services"
)

type RunnerController struct {
	svc services.NodeRunnerService
}

func NewRunnerController(svc services.NodeRunnerService) *RunnerController {
	return &RunnerController{svc: svc}
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
