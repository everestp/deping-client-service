package controllers

import (
	"database/sql"
	"encoding/json"
	"errors"
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

resp, err := c.svc.GetByEmailAndPubkey(r.Context(), email, req.Pubkey)
if err != nil {
	if errors.Is(err, sql.ErrNoRows) {
		respondError(w, http.StatusNotFound, "runner node not found")
		return
	}

	respondError(w, http.StatusInternalServerError, "internal server error")
	return
}

	respondJSON(w, http.StatusOK, resp)
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
