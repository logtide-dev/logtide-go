// Package otelexport provides a LogTide integration that bridges OpenTelemetry
// spans into the LogTide pipeline.
//
// Usage:
//
//	integration := otelexport.New()
//	flush := logtide.Init(logtide.ClientOptions{
//	    DSN:     "https://lp_abc@api.logtide.dev",
//	    Service: "my-service",
//	    Integrations: func(defaults []logtide.Integration) []logtide.Integration {
//	        return append(defaults, integration)
//	    },
//	})
//	defer flush()
//
//	// Register the span exporter with your TracerProvider:
//	tp := sdktrace.NewTracerProvider(
//	    sdktrace.WithBatcher(integration.Exporter()),
//	)
package otelexport

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	logtide "github.com/logtide-dev/logtide-sdk-go"
)

// Exporter implements sdktrace.SpanExporter.
// Each completed span is converted to a logtide.LogEntry and sent via the Client.
type Exporter struct {
	client *logtide.Client
}

// ExportSpans implements sdktrace.SpanExporter.
func (e *Exporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	if e.client == nil {
		return nil
	}
	for _, s := range spans {
		entry := spanToEntry(s)

		// Inject the span's trace context via a scope.
		// Use context.Background() as the base so no ambient active span
		// from the exporter's ctx can override the completed span's IDs:
		// captureEntry extracts OTel span IDs in step 2 (before the scope
		// is applied in step 3), so an active span in ctx would win.
		scope := logtide.NewScope(0)
		scope.SetTraceContext(entry.TraceID, entry.SpanID)
		spanCtx := logtide.WithScope(context.Background(), scope)

		if entry.Level == logtide.LevelError {
			e.client.Error(spanCtx, entry.Message, entry.Metadata)
		} else {
			e.client.Info(spanCtx, entry.Message, entry.Metadata)
		}
	}
	return nil
}

// Shutdown implements sdktrace.SpanExporter.
func (e *Exporter) Shutdown(ctx context.Context) error {
	if e.client != nil {
		e.client.Flush(ctx)
	}
	return nil
}

// Integration implements logtide.Integration and holds the Exporter reference.
// Call Exporter() after Setup has been called to obtain the configured exporter.
type Integration struct {
	exporter *Exporter
}

// New creates an Integration. Register it in ClientOptions.Integrations, then
// call Exporter() after Init to obtain the configured sdktrace.SpanExporter.
func New() *Integration {
	return &Integration{exporter: &Exporter{}}
}

// Name implements logtide.Integration.
func (i *Integration) Name() string { return "OTelSpanExport" }

// Setup implements logtide.Integration. Stores the client reference in the Exporter.
func (i *Integration) Setup(client *logtide.Client) {
	i.exporter.client = client
}

// Exporter returns the sdktrace.SpanExporter backed by this integration's Client.
// Register it with sdktrace.NewTracerProvider(sdktrace.WithBatcher(integration.Exporter())).
func (i *Integration) Exporter() *Exporter {
	return i.exporter
}

// --- span → LogEntry conversion ---

func spanToEntry(s sdktrace.ReadOnlySpan) *logtide.LogEntry {
	level := logtide.LevelInfo
	if s.Status().Code == codes.Error {
		level = logtide.LevelError
	}

	sc := s.SpanContext()
	meta := map[string]any{
		"span": map[string]any{
			"name":       s.Name(),
			"kind":       s.SpanKind().String(),
			"start_time": s.StartTime().Format(time.RFC3339Nano),
			"end_time":   s.EndTime().Format(time.RFC3339Nano),
			"duration_ms": s.EndTime().Sub(s.StartTime()).Milliseconds(),
			"status":     s.Status().Code.String(),
		},
	}

	if attrs := s.Attributes(); len(attrs) > 0 {
		attrMap := make(map[string]any, len(attrs))
		for _, a := range attrs {
			attrMap[string(a.Key)] = a.Value.AsInterface()
		}
		meta["attributes"] = attrMap
	}

	if events := s.Events(); len(events) > 0 {
		evList := make([]map[string]any, 0, len(events))
		for _, ev := range events {
			evList = append(evList, map[string]any{
				"name": ev.Name,
				"time": ev.Time.Format(time.RFC3339Nano),
			})
		}
		meta["events"] = evList
	}

	return &logtide.LogEntry{
		Level:    level,
		Message:  fmt.Sprintf("%s [%s]", s.Name(), s.SpanKind()),
		Metadata: meta,
		TraceID:  sc.TraceID().String(),
		SpanID:   sc.SpanID().String(),
	}
}
