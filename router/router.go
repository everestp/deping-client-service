package router

import (
	"encoding/json"
	"net/http"

	"github.com/everestp/deping-client-service/controllers"
	"github.com/everestp/deping-client-service/dto"
	"github.com/everestp/deping-client-service/services"
	"github.com/everestp/deping-client-service/ws"
)

// SetupRouter wires all HTTP routes using net/http ServeMux.
func SetupRouter(
	userCtrl *controllers.UserController,
	monitorCtrl *controllers.MonitorController,

	telegramCtrl *controllers.TelegramController,
	userService services.UserService,
	hub *ws.Hub,
) http.Handler {

	mux := http.NewServeMux()

	auth := JWTMiddleware(userService)

	// ─────────────────────────────────────────────────────────────────────
	// Health
	// ─────────────────────────────────────────────────────────────────────

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		_ = json.NewEncoder(w).Encode(map[string]string{
			"status": "ok",
		})
	})

	// ── WebSockets ────────────────────────────────────────────────────────
	// You will need to create a ServeWs function in your ws package
	// that handles the connection upgrade and hub registration.
	mux.HandleFunc("GET /ws", func(w http.ResponseWriter, r *http.Request) {
		ws.ServeWs(hub, w, r)
	})

	// ─────────────────────────────────────────────────────────────────────
	// Auth Routes
	// ─────────────────────────────────────────────────────────────────────

	mux.HandleFunc("POST /api/v1/auth/register", userCtrl.Register)
	mux.HandleFunc("POST /api/v1/auth/login", userCtrl.Login)

	mux.Handle(
		"GET /api/v1/auth/me",
		auth(http.HandlerFunc(userCtrl.Me)),
	)

	// ─────────────────────────────────────────────────────────────────────
	// Monitor Routes
	// ─────────────────────────────────────────────────────────────────────

	mux.Handle(
		"POST /api/v1/monitors",
		auth(http.HandlerFunc(monitorCtrl.Create)),
	)

	mux.Handle(
		"GET /api/v1/monitors",
		auth(http.HandlerFunc(monitorCtrl.List)),
	)

	mux.Handle(
		"GET /api/v1/monitors/{id}/stats",
		auth(http.HandlerFunc(monitorCtrl.Stats)),
	)

	mux.Handle(
		"PUT /api/v1/monitors/{id}/pause",
		auth(http.HandlerFunc(monitorCtrl.Pause)),
	)

	mux.Handle(
		"PUT /api/v1/monitors/{id}/resume",
		auth(http.HandlerFunc(monitorCtrl.Resume)),
	)

	mux.Handle(
		"DELETE /api/v1/monitors/{id}",
		auth(http.HandlerFunc(monitorCtrl.Delete)),
	)

	// ─────────────────────────────────────────────────────────────────────
	// Telegram Routes
	// ─────────────────────────────────────────────────────────────────────

	mux.Handle(
		"POST /api/v1/telegram/link",
		auth(http.HandlerFunc(telegramCtrl.InitiateLink)),
	)

	mux.Handle(
		"GET /api/v1/telegram/credits",
		auth(http.HandlerFunc(telegramCtrl.GetCredits)),
	)

	mux.Handle(
		"POST /api/v1/telegram/credits/add",
		auth(http.HandlerFunc(telegramCtrl.AddCredits)),
	)

	// monitor notification routes
	mux.Handle(
		"PUT /api/v1/monitors/{id}/notifications",
		auth(http.HandlerFunc(telegramCtrl.ToggleNotification)),
	)

	mux.Handle(
		"GET /api/v1/monitors/{id}/notifications",
		auth(http.HandlerFunc(telegramCtrl.GetNotificationStatus)),
	)

	// ─────────────────────────────────────────────────────────────────────
	// Runner Routes
	// ─────────────────────────────────────────────────────────────────────

	// mux.Handle(
	// 	"POST /api/v1/runner/register",
	// 	auth(http.HandlerFunc(runnerCtrl.Register)),
	// )

	// mux.Handle(
	// 	"GET /api/v1/runner/me",
	// 	auth(http.HandlerFunc(runnerCtrl.Me)),
	// )

	// mux.Handle(
	// 	"POST /api/v1/runner/heartbeat",
	// 	auth(http.HandlerFunc(runnerCtrl.Heartbeat)),
	// )

	// ─────────────────────────────────────────────────────────────────────
	// Ping Result Submission
	// ─────────────────────────────────────────────────────────────────────

	// mux.Handle(
	// 	"POST /api/v1/results",
	// 	auth(http.HandlerFunc(pingCtrl.SubmitResults)),
	// )

	// ─────────────────────────────────────────────────────────────────────
	// Reward Routes
	// ─────────────────────────────────────────────────────────────────────

	// mux.Handle(
	// 	"GET /api/v1/rewards/status",
	// 	auth(http.HandlerFunc(rewardCtrl.Status)),
	// )

	// ─────────────────────────────────────────────────────────────────────
	// 404 Fallback
	// ─────────────────────────────────────────────────────────────────────

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)

		_ = json.NewEncoder(w).Encode(dto.ErrorResponse{
			Error: "route not found",
		})
	})

	return mux
}
