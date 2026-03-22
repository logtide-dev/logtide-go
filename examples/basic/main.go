package main

import (
	"context"
	"log"
	"time"

	logtide "github.com/logtide-dev/logtide-sdk-go"
)

func main() {
	// Initialize LogTide — returns a flush func for deferred cleanup.
	flush := logtide.Init(logtide.ClientOptions{
		DSN:     "https://lp_your_api_key_here@api.logtide.dev",
		Service: "example-service",
		// Optional overrides:
		// BatchSize:     50,
		// FlushInterval: 10 * time.Second,
	})
	defer flush()

	ctx := context.Background()

	// Debug level — detailed debugging information.
	logtide.Debug(ctx, "Application started", map[string]any{
		"version":     "1.0.0",
		"environment": "production",
	})

	// Info level — general informational messages.
	logtide.Info(ctx, "User logged in", map[string]any{
		"user_id":  12345,
		"username": "john.doe",
		"ip":       "192.168.1.1",
	})

	// Warn level — warning messages.
	logtide.Warn(ctx, "High memory usage detected", map[string]any{
		"memory_usage_percent": 85,
		"threshold":            80,
	})

	// Error level — error events.
	logtide.Error(ctx, "Failed to connect to database", map[string]any{
		"database": "postgres",
		"host":     "db.example.com",
		"error":    "connection timeout after 30s",
		"retries":  3,
	})

	// Critical level — critical system errors.
	logtide.Critical(ctx, "System shutdown initiated", map[string]any{
		"reason": "critical error",
		"uptime": "72h",
	})

	// Log with nil metadata.
	logtide.Info(ctx, "Simple log without metadata", nil)

	// Simulate some work.
	log.Println("Doing some work...")
	time.Sleep(2 * time.Second)

	log.Println("Example completed successfully!")
}
