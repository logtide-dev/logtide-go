package logtide

import (
	"context"
	"fmt"
	"strings"

	"go.opentelemetry.io/otel/trace"
)

// ParseTraceparent parses a W3C traceparent header value:
//
//	00-{32 hex}-{16 hex}-{2 hex}
//
// Returns the extracted trace ID, span ID, and sampled flag.
// Returns an error if the header is malformed.
func ParseTraceparent(header string) (traceID, spanID string, sampled bool, err error) {
	parts := strings.Split(header, "-")
	if len(parts) != 4 || parts[0] != "00" {
		return "", "", false, fmt.Errorf("logtide: invalid traceparent %q", header)
	}
	if len(parts[1]) != 32 || len(parts[2]) != 16 || len(parts[3]) != 2 {
		return "", "", false, fmt.Errorf("logtide: invalid traceparent %q", header)
	}
	return parts[1], parts[2], parts[3] == "01", nil
}

// FormatTraceparent formats trace and span IDs into a W3C traceparent header.
func FormatTraceparent(traceID, spanID string, sampled bool) string {
	flags := "00"
	if sampled {
		flags = "01"
	}
	return fmt.Sprintf("00-%s-%s-%s", traceID, spanID, flags)
}

// traceContextFromContext extracts trace correlation from an active OTel span in ctx.
// Scope-based trace context is applied separately via Scope.ApplyToEntry.
func traceContextFromContext(ctx context.Context) (traceID, spanID string) {
	span := trace.SpanFromContext(ctx)
	if span != nil && span.SpanContext().IsValid() {
		sc := span.SpanContext()
		return sc.TraceID().String(), sc.SpanID().String()
	}
	return "", ""
}
