package router

import (
	"encoding/json"
	"net/http"

	"github.com/everestp/deping-client-service/controllers"
	"github.com/everestp/deping-client-service/dto"
	"github.com/everestp/deping-client-service/services"
)

// SetupRouter wires all HTTP routes with appropriate middleware.
// Uses the standard library ServeMux (Go 1.22+ path parameters).
func SetupRouter(
	userCtrl     *controllers.UserController,
	telegramCtrl *controllers.TelegramController,
	userService  services.UserService,
) http.Handler {
	mux := http.NewServeMux()

	auth := JWTMiddleware(userService)

	// ── Health ────────────────────────────────────────────────────────────
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	// ── Auth (public) ─────────────────────────────────────────────────────
	mux.HandleFunc("POST /api/auth/register", userCtrl.Register)
	mux.HandleFunc("POST /api/auth/login", userCtrl.Login)
	mux.Handle("GET /api/auth/me", auth(http.HandlerFunc(userCtrl.Me)))

	// ── Telegram account linking ──────────────────────────────────────────
	mux.Handle("POST /api/telegram/link",
		auth(http.HandlerFunc(telegramCtrl.InitiateLink)))

	mux.Handle("GET /api/telegram/credits",
		auth(http.HandlerFunc(telegramCtrl.GetCredits)))

	mux.Handle("POST /api/telegram/credits/add",
		auth(http.HandlerFunc(telegramCtrl.AddCredits)))

	// ── Per-monitor notification toggle ───────────────────────────────────
	mux.Handle("PUT /api/monitors/{monitor_id}/notifications",
		auth(http.HandlerFunc(telegramCtrl.ToggleNotification)))

	mux.Handle("GET /api/monitors/{monitor_id}/notifications",
		auth(http.HandlerFunc(telegramCtrl.GetNotificationStatus)))

	// ── 404 fallback ──────────────────────────────────────────────────────
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(dto.ErrorResponse{Error: "route not found"})
	})

	return mux
}
