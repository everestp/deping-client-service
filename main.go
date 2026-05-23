package main

import (
    "fmt"
    "os"
    "github.com/joho/godotenv"
    "github.com/everestp/deping-client-service/app"
    "github.com/everestp/deping-client-service/config/env"
)

func main() {
    // 1. Load .env file
    _ = godotenv.Load()

    // 2. Load configuration
    cfg, err := env.Load()
    if err != nil {
        fmt.Fprintf(os.Stderr, "config error: %v\n", err)
        os.Exit(1)
    }

    // 3. Initialize application (CORS is already handled inside app.New)
    application, err := app.New(cfg)
    if err != nil {
        fmt.Fprintf(os.Stderr, "init error: %v\n", err)
        os.Exit(1)
    }

    // 4. Start services
    if err := application.Run(); err != nil {
        fmt.Fprintf(os.Stderr, "runtime error: %v\n", err)
        os.Exit(1)
    }
}
