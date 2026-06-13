package router

import (
    "encoding/json"
    "net/http"

    "github.com/everestp/deping-client-service/config/env"
    "github.com/everestp/deping-client-service/controllers"
    "github.com/everestp/deping-client-service/dto"
    "github.com/everestp/deping-client-service/services"
    "github.com/everestp/deping-client-service/ws"
)

func SetupRouter(
    cfg *env.Config,
    userCtrl *controllers.UserController,
    monitorCtrl *controllers.MonitorController,
    telegramCtrl *controllers.TelegramController,
    runnerCtrl *controllers.RunnerController,
    txCtrl *controllers.TransactionController,
    userService services.UserService,
    hub *ws.Hub,
) http.Handler {

    mux := http.NewServeMux()
    auth := JWTMiddleware(cfg.JWTSecret)

    // ─────────────────────────────────────────────────────────────────────
    // Core Infrastructure
    // ─────────────────────────────────────────────────────────────────────
    mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "application/json")
        _ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
    })

    mux.HandleFunc("GET /ws", func(w http.ResponseWriter, r *http.Request) {
        ws.ServeWs(hub, w, r)
    })

    // ─────────────────────────────────────────────────────────────────────
    // Auth Routes
    // ─────────────────────────────────────────────────────────────────────
    mux.HandleFunc("POST /api/v1/auth/register", userCtrl.Register)
    mux.HandleFunc("POST /api/v1/auth/login", userCtrl.Login)
    mux.Handle("GET /api/v1/auth/me", auth(http.HandlerFunc(userCtrl.Me)))

    // ─────────────────────────────────────────────────────────────────────
    // Runner & Staking Routes
    // ─────────────────────────────────────────────────────────────────────
    mux.Handle("POST /api/v1/runner/register", auth(http.HandlerFunc(runnerCtrl.Register)))
    mux.Handle("POST /api/v1/runner/me", auth(http.HandlerFunc(runnerCtrl.Me)))
    mux.Handle("POST /api/v1/runner/heartbeat", auth(http.HandlerFunc(runnerCtrl.Heartbeat)))
    
    // Node Lifecycle
    mux.Handle("POST /api/v1/runner/activate", auth(http.HandlerFunc(runnerCtrl.ActivateNode)))
    mux.Handle("POST /api/v1/runner/stake", auth(http.HandlerFunc(runnerCtrl.ValidateStake)))
    mux.Handle("POST /api/v1/runner/unstake", auth(http.HandlerFunc(runnerCtrl.ValidateUnstake)))

    // ─────────────────────────────────────────────────────────────────────
    // Monitor & Telegram
    // ─────────────────────────────────────────────────────────────────────
    mux.Handle("POST /api/v1/monitors", auth(http.HandlerFunc(monitorCtrl.Create)))
    mux.Handle("GET /api/v1/monitors", auth(http.HandlerFunc(monitorCtrl.List)))
    mux.Handle("PUT /api/v1/monitors/{id}/notifications", auth(http.HandlerFunc(telegramCtrl.ToggleNotification)))
    mux.Handle("POST /api/v1/telegram/link", auth(http.HandlerFunc(telegramCtrl.InitiateLink)))
	 mux.Handle("POST /api/v1/telegram/credits/add", auth(http.HandlerFunc(telegramCtrl.AddCredits)))

    // ─────────────────────────────────────────────────────────────────────
    // Fallback
    // ─────────────────────────────────────────────────────────────────────
    mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusNotFound)
        _ = json.NewEncoder(w).Encode(dto.ErrorResponse{Error: "route not found"})
    })

    return mux
}