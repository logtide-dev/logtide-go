package main

import (
	"context"
	"log"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	logtide "github.com/logtide-dev/logtide-sdk-go"
)

func main() {
	// Set up OpenTelemetry tracer.
	exporter, err := stdouttrace.New(stdouttrace.WithPrettyPrint())
	if err != nil {
		log.Fatalf("Failed to create trace exporter: %v", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
	)
	defer func() {
		if err := tp.Shutdown(context.Background()); err != nil {
			log.Printf("Error shutting down tracer provider: %v", err)
		}
	}()

	otel.SetTracerProvider(tp)
	tracer := tp.Tracer("logtide-example")

	// Initialize LogTide.
	flush := logtide.Init(logtide.ClientOptions{
		DSN:     "https://lp_your_api_key_here@api.logtide.dev",
		Service: "otel-example",
	})
	defer flush()

	// Create client reference for helpers below.
	client := logtide.CurrentHub().Client()

	// Example 1: Root span.
	ctx := context.Background()
	ctx, span := tracer.Start(ctx, "main-operation")
	defer span.End()

	// This log will automatically include trace_id and span_id from OTel context.
	client.Info(ctx, "Starting main operation", map[string]any{
		"operation": "main",
	})

	// Simulate some work with nested spans.
	processOrder(ctx, tracer, client, "order-123")
	processPayment(ctx, tracer, client, "payment-456")

	client.Info(ctx, "Main operation completed", nil)

	log.Println("OpenTelemetry example completed!")
	log.Println("Check the console output to see trace IDs included in logs")
}

// processOrder simulates order processing with a child span.
func processOrder(ctx context.Context, tracer trace.Tracer, client *logtide.Client, orderID string) {
	ctx, span := tracer.Start(ctx, "process-order")
	defer span.End()

	client.Info(ctx, "Processing order", map[string]any{
		"order_id": orderID,
		"status":   "pending",
	})

	time.Sleep(100 * time.Millisecond)
	validateOrder(ctx, tracer, client, orderID)

	client.Info(ctx, "Order processed successfully", map[string]any{
		"order_id": orderID,
		"status":   "completed",
	})
}

// validateOrder simulates order validation with another child span.
func validateOrder(ctx context.Context, tracer trace.Tracer, client *logtide.Client, orderID string) {
	ctx, span := tracer.Start(ctx, "validate-order")
	defer span.End()

	client.Debug(ctx, "Validating order", map[string]any{"order_id": orderID})
	time.Sleep(50 * time.Millisecond)
	client.Debug(ctx, "Order validation completed", map[string]any{
		"order_id": orderID,
		"valid":    true,
	})
}

// processPayment simulates payment processing.
func processPayment(ctx context.Context, tracer trace.Tracer, client *logtide.Client, paymentID string) {
	ctx, span := tracer.Start(ctx, "process-payment")
	defer span.End()

	client.Info(ctx, "Processing payment", map[string]any{
		"payment_id": paymentID,
		"amount":     99.99,
		"currency":   "USD",
	})

	time.Sleep(150 * time.Millisecond)

	if paymentID == "payment-error" {
		client.Error(ctx, "Payment processing failed", map[string]any{
			"payment_id": paymentID,
			"error":      "insufficient funds",
		})
		return
	}

	client.Info(ctx, "Payment processed successfully", map[string]any{
		"payment_id":     paymentID,
		"transaction_id": "txn-789",
	})
}
