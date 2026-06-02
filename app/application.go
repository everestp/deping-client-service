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

	"github.com/everestp/deping-client-service/bot"
	"github.com/everestp/deping-client-service/config/env"
	"github.com/everestp/deping-client-service/controllers"
	"github.com/everestp/deping-client-service/db/repositories"
	"github.com/everestp/deping-client-service/router"
	"github.com/everestp/deping-client-service/services"
	"github.com/everestp/deping-client-service/solana"
	"github.com/everestp/deping-client-service/ws"
	_ "github.com/lib/pq"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/redis/go-redis/v9"
	"github.com/rs/cors"
)

// Application holds the dependency graph for the entire service.
type Application struct {
	cfg      *env.Config
	db       *sql.DB
	rdb      *redis.Client
	amqpConn *amqp.Connection
	bot      *bot.Bot
	httpSrv  *http.Server
	log      *slog.Logger
	consumer services.ConsumerService
	Hub      *ws.Hub
}

// New initializes the service and all its dependencies.
func New(cfg *env.Config) (*Application, error) {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	// 1. Initialize Infrastructure (DB, Redis, RabbitMQ)
	db, err := sql.Open("postgres", cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("db open: %w", err)
	}

	rdb := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr})
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		return nil, fmt.Errorf("redis ping: %w", err)
	}

	amqpConn, err := amqp.Dial(cfg.RabbitMQURL)
	if err != nil {
		return nil, fmt.Errorf("rabbitmq dial: %w", err)
	}

	rabbitCh, err := amqpConn.Channel()
	if err != nil {
		return nil, fmt.Errorf("rabbit channel: %w", err)
	}

	if err := declareQueues(rabbitCh); err != nil {
		return nil, fmt.Errorf("declare queues: %w", err)
	}

	// 2. Setup Storage Layer
	storage := repositories.NewStorage(db)
	memRegistry := services.NewMemoryRegistry()
	solanaClient := solana.NewSolanaClient(cfg.SolanaRPCURL)

	// 3. Initialize Domain Services
	userSvc := services.NewUserService(storage.Users, cfg.JWTSecret)
	teleSvc := services.NewTelegramService(storage.Telegram, cfg.TelegramBotUsername, log, solanaClient)
	monitorSvc := services.NewMonitorService(storage, rdb, rabbitCh, cfg)
runnerSvc := services.NewRunnerService(storage, rdb, rabbitCh, cfg, memRegistry)
	// 4. Initialize Bot & Alerts
	teleBot, err := bot.NewBot(cfg.TelegramBotToken, teleSvc, nil, storage.Telegram, log)
	if err != nil {
		return nil, fmt.Errorf("bot init: %w", err)
	}
	alertSvc := services.NewAlertService(storage.Telegram, teleBot, log)
	teleBot.SetAlertService(alertSvc)

	// 5. Initialize Consumer & WebSocket Bridge
	consumer, err := services.NewConsumerService(amqpConn, alertSvc, log)
	if err != nil {
		return nil, fmt.Errorf("consumer init: %w", err)
	}

	hub := ws.NewHub()
	go hub.Run() // Start background broadcaster
	ws.StartBridge(hub, rabbitCh)

	// 6. Controllers & Routing
	userCtrl := controllers.NewUserController(userSvc)
	teleCtrl := controllers.NewTelegramController(teleSvc)
	monitorCtrl := controllers.NewMonitorController(monitorSvc)
	runnerCtrl :=controllers.NewRunnerController(runnerSvc)
	txCtrl := controllers.NewTransactionController(solanaClient)

httpRouter := router.SetupRouter(
	cfg,
	userCtrl,
	monitorCtrl,
	teleCtrl,
	runnerCtrl,
	txCtrl,
	userSvc,
	hub,
)

	// 7. CORS Configuration
	c := cors.New(cors.Options{
		AllowedOrigins:   []string{"http://localhost:5173"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Authorization", "Content-Type"},
		AllowCredentials: true,
	})

	httpSrv := &http.Server{
		Addr:    ":" + cfg.HTTPPort,
		Handler: c.Handler(httpRouter),
	}

	return &Application{
		cfg:      cfg,
		db:       db,
		rdb:      rdb,
		amqpConn: amqpConn,
		bot:      teleBot,
		httpSrv:  httpSrv,
		log:      log,
		consumer: consumer,
		Hub:      hub,
	}, nil
}

// Run starts all background processes and the HTTP server.
func (a *Application) Run() error {
	a.log.Info("Service starting", "port", a.cfg.HTTPPort)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go a.bot.Start()
	go a.consumer.Start(ctx)

	// Start HTTP Server
	errChan := make(chan error, 1)
	go func() {
		if err := a.httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errChan <- err
		}
	}()

	// Graceful Shutdown block
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-quit:
		a.log.Info("Shutdown signal received")
	case err := <-errChan:
		a.log.Error("Server startup error", "err", err)
		return err
	}

	return a.shutdown()
}

func (a *Application) shutdown() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	a.bot.Stop()
	a.consumer.Stop()
	_ = a.httpSrv.Shutdown(ctx)
	_ = a.rdb.Close()
	_ = a.amqpConn.Close()
	return a.db.Close()
}

// declareQueues sets up the message broker infrastructure.
func declareQueues(ch *amqp.Channel) error {
	queues := []string{"job_queue", "processing_queue", "solana_sync_queue", "telegram_queue"}
	for _, q := range queues {
		if _, err := ch.QueueDeclare(q, true, false, false, false, nil); err != nil {
			return err
		}
	}

	exchange := "monitor_updates"
	if err := ch.ExchangeDeclare(exchange, "fanout", true, false, false, false, nil); err != nil {
		return err
	}

	for _, q := range []string{"processing_queue", "telegram_queue"} {
		if err := ch.QueueBind(q, "", exchange, false, nil); err != nil {
			return err
		}
	}
	return nil
}
