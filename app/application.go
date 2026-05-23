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

    // 1. Database Connection
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

    // 2. RabbitMQ Connection
    amqpConn, err := amqp.Dial(cfg.RabbitMQURL)
    if err != nil {
        return nil, fmt.Errorf("rabbitmq dial: %w", err)
    }
    log.Info("RabbitMQ connected")

    // 3. Dependency Graph
    storage := repositories.NewStorage(db)
    userService := services.NewUserService(storage.Users, cfg.JWTSecret)
    telegramService := services.NewTelegramService(storage.Telegram, cfg.TelegramBotUsername, log)

    // 4. Bot & Alert Service (Circular Dependency Resolution)
    teleBot, err := bot.NewBot(cfg.TelegramBotToken, telegramService, nil, storage.Telegram, log)
    if err != nil {
        return nil, fmt.Errorf("create bot: %w", err)
    }

    alertSvc := services.NewAlertService(storage.Telegram, teleBot, log)
    teleBot.SetAlertService(alertSvc)

    // 5. RabbitMQ Consumer
    consumer, err := services.NewConsumerService(amqpConn, alertSvc, log)
    if err != nil {
        return nil, fmt.Errorf("create consumer: %w", err)
    }

    // 6. HTTP Server
    userCtrl := controllers.NewUserController(userService)
    telegramCtrl := controllers.NewTelegramController(telegramService)
    httpRouter := router.SetupRouter(userCtrl, telegramCtrl, userService)

    c := cors.New(cors.Options{
        AllowedOrigins:   []string{"*"},
        AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
        AllowedHeaders:   []string{"Authorization", "Content-Type"},
        AllowCredentials: true,
    })

    httpSrv := &http.Server{
        Addr:         ":" + cfg.HTTPPort,
        Handler:      c.Handler(httpRouter),
        ReadTimeout:  15 * time.Second,
        WriteTimeout: 15 * time.Second,
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

func (a *Application) Run() error {
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()
    errCh := make(chan error, 3)

    // Start Services
    go func() {
        if err := a.httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
            errCh <- fmt.Errorf("http server: %w", err)
        }
    }()
    go func() { a.bot.Start() }()
    go func() {
        if err := a.consumer.Start(ctx); err != nil {
            errCh <- fmt.Errorf("consumer: %w", err)
        }
    }()

    // Wait for shutdown
    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

    select {
    case sig := <-quit:
        a.log.Info("shutdown signal", "sig", sig)
    case err := <-errCh:
        a.log.Error("fatal error", "err", err)
        return err
    }

    return a.shutdown()
}

func (a *Application) shutdown() error {
    a.log.Info("shutting down...")
    ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
    defer cancel()

    _ = a.httpSrv.Shutdown(ctx)
    a.bot.Stop()
    a.consumer.Stop()
    _ = a.amqpConn.Close()
    return a.db.Close()
}
