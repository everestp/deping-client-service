package app

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/lib/pq"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/rs/cors"

	"github.com/everestp/deping-client-service/bot"
	"github.com/everestp/deping-client-service/config/env"
	"github.com/everestp/deping-client-service/controllers"
	"github.com/everestp/deping-client-service/db/repositories"
	"github.com/everestp/deping-client-service/router"
	"github.com/everestp/deping-client-service/services"
)

// Application is the root dependency container.
// All construction happens here; everything else just receives interfaces.
type Application struct {
	cfg      *env.Config
	db       *sql.DB
	amqpConn *amqp.Connection
	httpSrv  *http.Server
	bot      *bot.Bot
	consumer services.ConsumerService
	log      *slog.Logger
}

func New(cfg *env.Config) (*Application, error) {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	// ── Database ──────────────────────────────────────────────────────────
	db, err := sql.Open("postgres", cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(5 * time.Minute)

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping db: %w", err)
	}
	log.Info("database connected")

	// ── RabbitMQ ──────────────────────────────────────────────────────────
	amqpConn, err := amqp.Dial(cfg.RabbitMQURL)
	if err != nil {
		return nil, fmt.Errorf("rabbitmq dial: %w", err)
	}
	log.Info("RabbitMQ connected")

	// ── Repositories ──────────────────────────────────────────────────────
	storage := repositories.NewStorage(db)

	// ── Services ──────────────────────────────────────────────────────────
	userService := services.NewUserService(storage.Users, cfg.JWTSecret)
	telegramService := services.NewTelegramService(storage.Telegram, cfg.TelegramBotUsername, log)

	// ── Telegram Bot ──────────────────────────────────────────────────────
	// AlertService needs a MessageSender (the bot).
	// We break the chicken-and-egg by creating the bot first, then injecting it.
	//
	// Bot → AlertService → Bot (via MessageSender interface)
	// We resolve this with a pointer swap after both are constructed.

	var alertSvc services.AlertService // declared first

	teleBot, err := bot.NewBot(
		cfg.TelegramBotToken,
		telegramService,
		// alertSvc is nil here momentarily — NewBot doesn't call it during construction
		nil,
		storage.Telegram,
		log,
	)
	if err != nil {
		return nil, fmt.Errorf("create bot: %w", err)
	}

	// Now create AlertService with the real MessageSender
	alertSvc = services.NewAlertService(storage.Telegram, teleBot, log)

	// Patch bot's alert service reference (bot stores it for forwarding direct messages)
	teleBot.SetAlertService(alertSvc)

	// ── RabbitMQ Consumer ─────────────────────────────────────────────────
	consumer, err := services.NewConsumerService(amqpConn, alertSvc, log)
	if err != nil {
		return nil, fmt.Errorf("create consumer: %w", err)
	}

	// ── HTTP Controllers & Router ─────────────────────────────────────────
	userCtrl := controllers.NewUserController(userService)
	telegramCtrl := controllers.NewTelegramController(telegramService)

	httpRouter := router.SetupRouter(userCtrl, telegramCtrl, userService)
c := cors.New(cors.Options{
        AllowedOrigins:   []string{"*"}, // Change to your frontend domain in production
        AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
        AllowedHeaders:   []string{"Authorization", "Content-Type"},
        AllowCredentials: true,
    })

	handler := c.Handler(httpRouter)
	httpSrv := &http.Server{
		Addr:         ":" + cfg.HTTPPort,
		Handler:      handler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	return &Application{
		cfg:      cfg,
		db:       db,
		amqpConn: amqpConn,
		httpSrv:  httpSrv,
		bot:      teleBot,
		consumer: consumer,
		log:      log,
	}, nil
}

// Run starts all services and blocks until a shutdown signal is received.
func (a *Application) Run() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 3)

	// ── HTTP Server ───────────────────────────────────────────────────────
	go func() {
		a.log.Info("HTTP server starting", "port", a.cfg.HTTPPort)
		if err := a.httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- fmt.Errorf("http server: %w", err)
		}
	}()

	// ── Telegram Bot ──────────────────────────────────────────────────────
	go func() {
		a.log.Info("Telegram bot starting")
		a.bot.Start() // blocks until bot.Stop() is called
	}()

	// ── RabbitMQ Consumer ─────────────────────────────────────────────────
	go func() {
		if err := a.consumer.Start(ctx); err != nil {
			errCh <- fmt.Errorf("consumer: %w", err)
		}
	}()

	// ── Shutdown on signal ────────────────────────────────────────────────
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-quit:
		a.log.Info("shutdown signal received", "signal", sig.String())
	case err := <-errCh:
		a.log.Error("fatal error", "err", err)
		return err
	}

	return a.shutdown()
}

func (a *Application) shutdown() error {
	a.log.Info("shutting down gracefully...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Stop HTTP server
	if err := a.httpSrv.Shutdown(shutdownCtx); err != nil {
		a.log.Error("http shutdown error", "err", err)
	}

	// Stop Telegram bot
	a.bot.Stop()

	// Stop consumer
	a.consumer.Stop()

	// Close RabbitMQ
	if err := a.amqpConn.Close(); err != nil {
		a.log.Error("amqp close error", "err", err)
	}

	// Close DB
	if err := a.db.Close(); err != nil {
		a.log.Error("db close error", "err", err)
	}

	a.log.Info("shutdown complete")
	return nil
}
