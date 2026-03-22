package logtide

import (
	"errors"
	"fmt"
)

var (
	// ErrClientClosed indicates that a Client has been closed. After Close(),
	// log-level methods silently drop entries and return an empty EventID.
	// This sentinel is available for custom Transport or middleware implementations
	// that need to surface the closed-client condition explicitly.
	ErrClientClosed = errors.New("logtide: client is closed")

	// ErrInvalidDSN is returned when a DSN string cannot be parsed.
	ErrInvalidDSN = errors.New("logtide: invalid DSN")

	// ErrCircuitOpen is returned when the circuit breaker is in the open state.
	ErrCircuitOpen = errors.New("logtide: circuit breaker is open")

	// ErrServiceRequired is returned when the service name is not set.
	ErrServiceRequired = errors.New("logtide: service name is required")
)

// ValidationError represents a field-level validation failure.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("logtide: validation error on field %q: %s", e.Field, e.Message)
}

// HTTPError represents an unexpected HTTP response from the ingest endpoint.
type HTTPError struct {
	StatusCode int
	Message    string
	Body       string
}

func (e *HTTPError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("logtide: HTTP %d: %s", e.StatusCode, e.Message)
	}
	return fmt.Sprintf("logtide: HTTP %d", e.StatusCode)
}
