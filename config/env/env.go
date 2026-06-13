package env

import (
	"fmt"
	"os"
	"strconv"
	"sync"
)

type Config struct {
	DatabaseURL         string
	HTTPPort            string
	JWTSecret           string
	RabbitMQURL         string
	QueueName           string
	TelegramBotToken    string
	TelegramBotUsername string
	RedisAddr           string
	SolanaRPCURL        string
	DepingMintAddress   string
	StakeTreasuryAddr   string
}

var (
	cfg  *Config
	once sync.Once
	err  error
)

// Load initializes config ONLY ONCE (thread-safe singleton)
func Load() (*Config, error) {
	once.Do(func() {

		cfg, err = loadConfig()
	})

	return cfg, err
}

// MUST call internally only once
func loadConfig() (*Config, error) {

	dbURL, err := requireEnv("DATABASE_URL")
	if err != nil {
		return nil, err
	}

	jwtSecret, err := requireEnv("JWT_SECRET")
	if err != nil {
		return nil, err
	}

	if len(jwtSecret) < 16 {
		return nil, fmt.Errorf("JWT_SECRET too short (min 16 chars)")
	}

	rabbitURL, err := requireEnv("RABBITMQ_URL")
	if err != nil {
		return nil, err
	}

	redisAddr, err := requireEnv("REDIS_ADDR")
	if err != nil {
		return nil, err
	}

	botToken, err := requireEnv("TELEGRAM_BOT_TOKEN")
	if err != nil {
		return nil, err
	}

	cfg := &Config{
		DatabaseURL:         dbURL,
		HTTPPort:            getEnvOr("HTTP_PORT", "8081"),
		JWTSecret:           jwtSecret,
		RabbitMQURL:         rabbitURL,
		RedisAddr:           redisAddr,
		QueueName:           getEnvOr("RABBITMQ_QUEUE", "rabbit_mq_queue"),
		TelegramBotToken:    botToken,
		TelegramBotUsername: getEnvOr("TELEGRAM_BOT_USERNAME", "depingnetworkbot"),
		SolanaRPCURL:        getEnvOr("SOLANA_RPC_URL", "https://api.devnet.solana.com"),
		DepingMintAddress:   getEnvOr("DEPING_MINT_ADDRESS", "2V5HdggYQXW1Z9nhrVKjNdYqg5NsQnZhwMERYr8WK1pU"),
		StakeTreasuryAddr:   getEnvOr("STAKE_TREASURY_WALLET", "3pnWN58LE6vofXJKqv93Uj5NvcE7qxN6jiXgskaNhgkF"),
	}

	// validate port
	port, err := strconv.Atoi(cfg.HTTPPort)
	if err != nil || port < 1 || port > 65535 {
		return nil, fmt.Errorf("invalid HTTP_PORT: %q", cfg.HTTPPort)
	}

	return cfg, nil
}

// PUBLIC ACCESS (clean usage everywhere)
func Get() *Config {
	if cfg == nil {
		panic("env not initialized: call env.Load() once at startup")
	}
	return cfg
}

// helpers
func requireEnv(key string) (string, error) {
	v := os.Getenv(key)
	if v == "" {
		return "", fmt.Errorf("missing env: %s", key)
	}
	return v, nil
}

func getEnvOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
