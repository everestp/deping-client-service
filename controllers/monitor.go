package controllers

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/everestp/deping-client-service/dto"
	"github.com/everestp/deping-client-service/pkg/contextutils"
	"github.com/everestp/deping-client-service/services"
)

type MonitorController struct {
	svc services.MonitorService
}

func NewMonitorController(svc services.MonitorService) *MonitorController {
	return &MonitorController{svc: svc}
}

// Helper to extract ID from URL path (e.g., /api/monitors/{id})
func extractID(r *http.Request) string {
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return ""
}

// --- Helper Functions ---

func respondJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(payload)
}

func respondError(w http.ResponseWriter, status int, message string) {
	respondJSON(w, status, dto.ErrorResponse{Error: message})
}

// --- Handlers ---

func (c *MonitorController) Create(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req dto.CreateMonitorRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	ownerID := contextutils.GetUserID(r.Context())
	resp, err := c.svc.Create(r.Context(), ownerID, &req)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to create monitor")
		return
	}
	respondJSON(w, http.StatusCreated, resp)
}

func (c *MonitorController) List(w http.ResponseWriter, r *http.Request) {
	ownerID := contextutils.GetUserID(r.Context())
	resp, err := c.svc.ListByOwner(r.Context(), ownerID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to fetch monitors")
		return
	}
	respondJSON(w, http.StatusOK, resp)
}

func (c *MonitorController) Stats(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
    if id == "" {
        respondError(w, http.StatusBadRequest, "missing id")
        return
    }
	ownerID := contextutils.GetUserID(r.Context())
	resp, err := c.svc.Stats(r.Context(), id, ownerID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to fetch stats")
		return
	}
	respondJSON(w, http.StatusOK, resp)
}

func (c *MonitorController) Pause(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
    if id == "" {
        respondError(w, http.StatusBadRequest, "missing id")
        return
    }
	ownerID := contextutils.GetUserID(r.Context())
	if err := c.svc.Pause(r.Context(), id, ownerID); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to pause monitor")
		return
	}
	respondJSON(w, http.StatusOK, dto.MessageResponse{Message: "monitor paused"})
}

func (c *MonitorController) Resume(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
    if id == "" {
        respondError(w, http.StatusBadRequest, "missing id")
        return
    }
	ownerID := contextutils.GetUserID(r.Context())
	if err := c.svc.Resume(r.Context(), id, ownerID); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to resume monitor")
		return
	}
	respondJSON(w, http.StatusOK, dto.MessageResponse{Message: "monitor resumed"})
}

func (c *MonitorController) Delete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
    if id == "" {
        respondError(w, http.StatusBadRequest, "missing id")
        return
    }
	ownerID := contextutils.GetUserID(r.Context())
	if err := c.svc.Delete(r.Context(), id, ownerID); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to delete monitor")
		return
	}
	respondJSON(w, http.StatusOK, dto.MessageResponse{Message: "monitor deleted"})
}
