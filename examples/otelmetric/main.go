// Command otelmetric demonstrates exporting OpenTelemetry metrics through
// LogTide. Counters, gauges and histograms recorded against the MeterProvider
// are converted to LogTide log entries carrying the same service name,
// environment, tags and resource attributes as your logs and traces.
package main

import (
	"context"
	"log"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	logtide "github.com/logtide-dev/logtide-sdk-go"
	"github.com/logtide-dev/logtide-sdk-go/integrations/otelmetric"
)

func main() {
	// Create the metric integration and register it with LogTide.
	integration := otelmetric.New()

	flush := logtide.Init(logtide.ClientOptions{
		DSN:     "https://lp_your_api_key_here@api.logtide.dev",
		Service: "otelmetric-example",
		Integrations: func(defaults []logtide.Integration) []logtide.Integration {
			return append(defaults, integration)
		},
	})
	defer flush()

	// Wire the exporter into a MeterProvider. A PeriodicReader collects metrics
	// on an interval and hands them to the exporter.
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(
			sdkmetric.NewPeriodicReader(
				integration.Exporter(),
				sdkmetric.WithInterval(2*time.Second),
			),
		),
	)
	defer func() {
		if err := mp.Shutdown(context.Background()); err != nil {
			log.Printf("Error shutting down meter provider: %v", err)
		}
	}()
	otel.SetMeterProvider(mp)

	meter := mp.Meter("logtide-metric-example")

	// A counter.
	requests, err := meter.Int64Counter(
		"http.requests",
		metric.WithDescription("Number of handled HTTP requests"),
		metric.WithUnit("1"),
	)
	if err != nil {
		log.Fatalf("create counter: %v", err)
	}

	// A histogram.
	latency, err := meter.Float64Histogram(
		"http.latency",
		metric.WithDescription("Request latency"),
		metric.WithUnit("ms"),
	)
	if err != nil {
		log.Fatalf("create histogram: %v", err)
	}

	ctx := context.Background()

	// Record some measurements.
	for i := 0; i < 5; i++ {
		requests.Add(ctx, 1)
		latency.Record(ctx, float64(40+i*7))
		time.Sleep(200 * time.Millisecond)
	}

	// Force a final collection before exit so the metrics are flushed.
	if err := mp.ForceFlush(ctx); err != nil {
		log.Printf("force flush: %v", err)
	}

	log.Println("OpenTelemetry metrics example completed!")
	log.Println("Counter and histogram values were exported to LogTide as log entries.")
}
