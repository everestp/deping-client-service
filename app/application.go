package app

import (
	"context"
	"database/sql"
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
	_ "github.com/lib/pq"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/redis/go-redis/v9"
	"github.com/rs/cors"
)

type Application struct {
    cfg      *env.Config
    db       *sql.DB
    rdb      *redis.Client
    amqpConn *amqp.Connection
    bot      *bot.Bot
    httpSrv  *http.Server
    log      *slog.Logger
    consumer  services.ConsumerService
}

func New(cfg *env.Config) (*Application, error) {
    log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

    // 1. Core Infrastructure
    db, _ := sql.Open("postgres", cfg.DatabaseURL)
    rdb := redis.NewClient(&redis.Options{Addr: cfg.RabbitMQURL})
    amqpConn, _ := amqp.Dial(cfg.RabbitMQURL)
    rabbitCh, _ := amqpConn.Channel()

    // 2. Storage Layer
    storage := repositories.NewStorage(db)

    // 3. Services
    userSvc := services.NewUserService(storage.Users, cfg.JWTSecret)
    teleSvc := services.NewTelegramService(storage.Telegram, cfg.TelegramBotUsername, log)
    monitorSvc := services.NewMonitorService(storage, rdb, rabbitCh, cfg)

    // 4. Bot Initialization
    teleBot, _ := bot.NewBot(cfg.TelegramBotToken, teleSvc, nil, storage.Telegram, log)
    alertSvc := services.NewAlertService(storage.Telegram, teleBot, log)
    teleBot.SetAlertService(alertSvc)
    // 5. Consumer Initialization (REQUIRED for pings to process)

    consumer, err := services.NewConsumerService(amqpConn, alertSvc, log)
    if err != nil { return nil, err }

    // 6. Controllers
    userCtrl := controllers.NewUserController(userSvc)
    teleCtrl := controllers.NewTelegramController(teleSvc)
    monitorCtrl := controllers.NewMonitorController(monitorSvc)

    // 6. HTTP Server Setup
    httpRouter := router.SetupRouter(userCtrl, monitorCtrl, teleCtrl, userSvc)

    httpSrv := &http.Server{
        Addr:    ":" + cfg.HTTPPort,
        Handler: cors.Default().Handler(httpRouter),
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
    }, nil
}

func (a *Application) Run() error {
    a.log.Info("Service starting", "port", a.cfg.HTTPPort)
ctx, cancel := context.WithCancel(context.Background())
    defer cancel()
    go func() { a.bot.Start() }()
    go a.consumer.Start(ctx) //  Start consumer in background


    go func() {
        if err := a.httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
            a.log.Error("server failed", "err", err)
        }
    }()

    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    <-quit

    return a.shutdown()
}

func (a *Application) shutdown() error {
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()

    a.bot.Stop()
    a.httpSrv.Shutdown(ctx)
    a.rdb.Close()
    a.amqpConn.Close()
    return a.db.Close()
}
