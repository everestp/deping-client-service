package controllers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"

	"github.com/everestp/deping-client-service/dto"
	"github.com/everestp/deping-client-service/pkg/contextutils"

	"github.com/everestp/deping-client-service/services"
)

// TelegramController handles HTTP endpoints for Telegram integration.
// These are called from the web frontend (dashboard).
type TelegramController struct {
	telegramService services.TelegramService
}

func NewTelegramController(ts services.TelegramService) *TelegramController {
	return &TelegramController{telegramService: ts}
}

// POST /api/telegram/link
// Body: { "telegram_username": "@alice" }
// Requires: JWT auth middleware (sets user_id in context)
//
// Returns a verification code the user must send to the bot.
func (tc *TelegramController) InitiateLink(w http.ResponseWriter, r *http.Request) {
    userID := userIDFromContext(r.Context())
	

    var req dto.LinkTelegramRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TelegramUsername == "" {
        writeJSON(w, http.StatusBadRequest, dto.ErrorResponse{Error: "telegram_username is required"})
        return
    }

    resp, err := tc.telegramService.InitiateLink(r.Context(), userID, req.TelegramUsername)
    if err != nil {
        // Log the actual error to your terminal
        fmt.Errorf("InitiateLink failed", "error", err, "userID", userID)

        // Return the specific error to the frontend (temporarily)
        writeJSON(w, http.StatusInternalServerError, dto.ErrorResponse{Error: err.Error()})
        return
    }

    writeJSON(w, http.StatusOK, resp)
}
func (tc *TelegramController) GetTelegramMe(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r.Context())

	// Call the service layer we built in step 1
	resp, err := tc.telegramService.GetTelegramUserStatus(r.Context(), userID)
	if err != nil {
		log.Printf("[ERROR] GetTelegramUserStatus failed for userID %d: %v", userID, err)
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"success": false,
			"error":   "Internal server error",
		})
		return
	}

	// Always returns 200 OK with success: true. Data will either be the user object or null.
	writeJSON(w, http.StatusOK, resp)
}
// GET /api/telegram/credits
// Returns the authenticated user's credit status.
func (tc *TelegramController) GetCredits(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r.Context())

	status, err := tc.telegramService.GetCreditStatus(r.Context(), userID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, dto.ErrorResponse{Error: "could not fetch credits"})
		return
	}

	writeJSON(w, http.StatusOK, status)
}

// POST /api/telegram/credits/add
// Body: { "amount": 100, "tx_signature": "5Kj..." }
// Called after Solana SPL payment is confirmed on-chain.
func (tc *TelegramController) AddCredits(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r.Context())

	var req dto.AddCreditsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Amount <= 0 || req.TxSignature == "" {
		writeJSON(w, http.StatusBadRequest, dto.ErrorResponse{Error: "amount and tx_signature are required"})
		return
	}

	newBalance, err := tc.telegramService.AddPurchasedCredits(r.Context(), userID, req.Amount, req.TxSignature)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, dto.ErrorResponse{Error: "could not add credits"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message":           "Credits added successfully",
		"purchased_credits": newBalance,
	})
}

// PUT /api/monitors/{monitor_id}/notifications
// Body: { "is_notifications_enabled": true }
// Toggles Telegram alerts for a specific monitor.
func (tc *TelegramController) ToggleNotification(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r.Context())

	// Extract monitor_id from URL path — adjust to your router's pattern
	monitorID := r.PathValue("monitor_id")
	if monitorID == "" {
		writeJSON(w, http.StatusBadRequest, dto.ErrorResponse{Error: "monitor_id is required"})
		return
	}

	var req dto.ToggleNotificationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, dto.ErrorResponse{Error: "invalid request body"})
		return
	}

	if err := tc.telegramService.ToggleNotification(r.Context(), userID, monitorID, req.IsNotificationsEnabled); err != nil {
		writeJSON(w, http.StatusInternalServerError, dto.ErrorResponse{Error: "could not update notification preference"})
		return
	}

	writeJSON(w, http.StatusOK, dto.SubscriptionResponse{
		MonitorID:              monitorID,
		IsNotificationsEnabled: req.IsNotificationsEnabled,
	})
}

// GET /api/monitors/{monitor_id}/notifications
// Returns the current notification toggle state.
func (tc *TelegramController) GetNotificationStatus(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r.Context())
	monitorID := r.PathValue("monitor_id")
	if monitorID == "" {
		writeJSON(w, http.StatusBadRequest, dto.ErrorResponse{Error: "monitor_id is required"})
		return
	}

	enabled, err := tc.telegramService.GetNotificationStatus(r.Context(), userID, monitorID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, dto.ErrorResponse{Error: "could not fetch status"})
		return
	}

	writeJSON(w, http.StatusOK, dto.SubscriptionResponse{
		MonitorID:              monitorID,
		IsNotificationsEnabled: enabled,
	})
}

// ── Helpers ───────────────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// userIDFromContext retrieves the user_id set by JWT middleware.
// Adjust the key type to match your middleware implementation.
func userIDFromContext(ctx interface{ Value(key interface{}) interface{} }) int {
    // Look up using the EXACT constant used by the middleware
v := ctx.Value(contextutils.UserIDKey)
    if v == nil {
        return 0
    }

    switch id := v.(type) {
    case int:
        return id
    case float64:
        return int(id)
    case string:
        n, _ := strconv.Atoi(id)
        return n
    default:
        return 0
    }
}
