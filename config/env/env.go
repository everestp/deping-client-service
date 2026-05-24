package env

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	DatabaseURL         string
	HTTPPort            string
	JWTSecret           string
	RabbitMQURL         string
	QueueName           string
	TelegramBotToken    string
	TelegramBotUsername string
}

// Load reads all required environment variables and returns an error if missing.
func Load() (*Config, error) {
	dbURL, err := requireEnv("DATABASE_URL")
	if err != nil {
		return nil, err
	}

	jwtSecret, err := requireEnv("JWT_SECRET")
	if err != nil {
		return nil, err
	}

	rabbitURL, err := requireEnv("RABBITMQ_URL")
	if err != nil {
		return nil, err
	}

	botToken, err := requireEnv("TELEGRAM_BOT_TOKEN")
	if err != nil {
		return nil, err
	}

	c := &Config{
		DatabaseURL:         dbURL,
		HTTPPort:            getEnvOr("HTTP_PORT", "8080"),
		JWTSecret:           jwtSecret,
		RabbitMQURL:         rabbitURL,
		QueueName:           getEnvOr("RABBITMQ_QUEUE", "telegram_queue"),
		TelegramBotToken:    botToken,
		TelegramBotUsername: getEnvOr("TELEGRAM_BOT_USERNAME", "depingnetworkbot"),
	}

	// Validate numeric-looking fields
	port, err := strconv.Atoi(c.HTTPPort)
	if err != nil || port < 1 || port > 65535 {
		return nil, fmt.Errorf("HTTP_PORT must be a valid port number (1-65535), got: %q", c.HTTPPort)
	}

	return c, nil
}

// requireEnv returns an error instead of panicking.
func requireEnv(key string) (string, error) {
	v := os.Getenv(key)
	if v == "" {
		return "", fmt.Errorf("required environment variable %q is not set", key)
	}
	return v, nil
}

func getEnvOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func GetString(key, fallback string) string {
	return getEnvOr(key, fallback)
}
