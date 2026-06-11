package logtide

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"time"
)

// sdkVersion is embedded in the User-Agent header and SDK metadata.
const sdkVersion = "0.9.4"

// Client sends log entries to the LogTide ingest endpoint.
// Use NewClient for the explicit-lifecycle pattern, or Init + package-level
// functions for the global singleton pattern.
//
// Client is safe for concurrent use.
type Client struct {
	opts        ClientOptions
	dsn         *DSN
	serverName  string
	transport   Transport
	integrations []Integration
	processors  []EventProcessor

	mu     sync.RWMutex
	closed bool
}

// NewClient creates and configures a Client from opts.
//
// It parses opts.DSN, constructs the default HTTPTransport (unless
// opts.Transport is set), installs integrations, and validates required fields.
func NewClient(opts ClientOptions) (*Client, error) {
	if opts.Service == "" {
		return nil, ErrServiceRequired
	}

	// Apply defaults for zero values.
	defaults := NewClientOptions()
	if opts.MaxBreadcrumbs <= 0 {
		opts.MaxBreadcrumbs = defaults.MaxBreadcrumbs
	}
	if opts.SampleRate == 0 {
		opts.SampleRate = defaults.SampleRate
	}
	if opts.BatchSize <= 0 {
		opts.BatchSize = defaults.BatchSize
	}
	if opts.FlushInterval <= 0 {
		opts.FlushInterval = defaults.FlushInterval
	}
	if opts.FlushTimeout <= 0 {
		opts.FlushTimeout = defaults.FlushTimeout
	}
	if opts.MaxRetries <= 0 {
		opts.MaxRetries = defaults.MaxRetries
	}
	if opts.RetryMinBackoff <= 0 {
		opts.RetryMinBackoff = defaults.RetryMinBackoff
	}
	if opts.RetryMaxBackoff <= 0 {
		opts.RetryMaxBackoff = defaults.RetryMaxBackoff
	}
	if opts.CircuitBreakerThreshold <= 0 {
		opts.CircuitBreakerThreshold = defaults.CircuitBreakerThreshold
	}
	if opts.CircuitBreakerTimeout <= 0 {
		opts.CircuitBreakerTimeout = defaults.CircuitBreakerTimeout
	}
	if opts.AttachStacktrace == nil {
		opts.AttachStacktrace = defaults.AttachStacktrace
	}

	var dsn *DSN
	if opts.Transport == nil {
		// DSN is required when using the default transport.
		if opts.DSN == "" {
			return nil, fmt.Errorf("logtide: DSN is required (set ClientOptions.DSN or provide a custom Transport)")
		}
		var err error
		dsn, err = ParseDSN(opts.DSN)
		if err != nil {
			return nil, err
		}
	}

	c := &Client{
		opts:       opts,
		dsn:        dsn,
		serverName: resolveServerName(opts.ServerName),
	}

	if opts.Transport != nil {
		c.transport = opts.Transport
	} else {
		c.transport = newHTTPTransport(dsn, opts)
	}

	setupIntegrations(c, opts)
	return c, nil
}

// Integrations returns the list of integrations installed on this client,
// in the order they were registered. The slice is a copy.
func (c *Client) Integrations() []Integration {
	c.mu.RLock()
	result := make([]Integration, len(c.integrations))
	copy(result, c.integrations)
	c.mu.RUnlock()
	return result
}

// AddEventProcessor appends a processor to the client-level pipeline.
// Called by integrations from their Setup method.
func (c *Client) AddEventProcessor(p EventProcessor) {
	c.mu.Lock()
	c.processors = append(c.processors, p)
	c.mu.Unlock()
}

// Options returns a copy of the client's configuration.
// The Tags map is copied so callers cannot inadvertently mutate internal state.
func (c *Client) Options() ClientOptions {
	c.mu.RLock()
	opts := c.opts
	if len(opts.Tags) > 0 {
		tags := make(map[string]string, len(opts.Tags))
		for k, v := range opts.Tags {
			tags[k] = v
		}
		opts.Tags = tags
	}
	c.mu.RUnlock()
	return opts
}

// Debug captures a debug-level log entry.
// Returns the EventID assigned to the entry, or "" if it was dropped or the client is closed.
func (c *Client) Debug(ctx context.Context, message string, metadata map[string]any) EventID {
	return c.log(ctx, LevelDebug, message, metadata)
}

// Info captures an info-level log entry.
// Returns the EventID assigned to the entry, or "" if it was dropped or the client is closed.
func (c *Client) Info(ctx context.Context, message string, metadata map[string]any) EventID {
	return c.log(ctx, LevelInfo, message, metadata)
}

// Warn captures a warn-level log entry.
// Returns the EventID assigned to the entry, or "" if it was dropped or the client is closed.
func (c *Client) Warn(ctx context.Context, message string, metadata map[string]any) EventID {
	return c.log(ctx, LevelWarn, message, metadata)
}

// Error captures an error-level log entry.
// Returns the EventID assigned to the entry, or "" if it was dropped or the client is closed.
func (c *Client) Error(ctx context.Context, message string, metadata map[string]any) EventID {
	return c.log(ctx, LevelError, message, metadata)
}

// Critical captures a critical-level log entry.
// Returns the EventID assigned to the entry, or "" if it was dropped or the client is closed.
func (c *Client) Critical(ctx context.Context, message string, metadata map[string]any) EventID {
	return c.log(ctx, LevelCritical, message, metadata)
}

// CaptureError captures err as an error-level log entry with an attached
// stack trace. It serialises the full error chain via errors.Unwrap.
// Returns the EventID of the entry, or "" if it was dropped.
func (c *Client) CaptureError(ctx context.Context, err error, metadata map[string]any) EventID {
	if err == nil {
		return ""
	}

	exceptions := extractExceptions(err, c.opts.AttachStacktrace != nil && *c.opts.AttachStacktrace)

	entry := &LogEntry{
		Level:   LevelError,
		Message: err.Error(),
		Errors:  exceptions,
	}

	if metadata != nil {
		entry.Metadata = metadata
	}

	return c.captureEntry(ctx, entry, &EventHint{OriginalError: err})
}

// CaptureEntry runs the full capture pipeline (scope merge, processors,
// BeforeSend, sampling) for a caller-built LogEntry and dispatches it.
// Level defaults to info and message is required. Returns the assigned
// EventID, or "" if the entry was dropped.
func (c *Client) CaptureEntry(ctx context.Context, entry *LogEntry) EventID {
	if entry == nil {
		return ""
	}
	c.mu.RLock()
	closed := c.closed
	c.mu.RUnlock()
	if closed {
		return ""
	}
	if entry.Level == "" {
		entry.Level = LevelInfo
	}
	return c.captureEntry(ctx, entry, nil)
}

// ExceptionsFromError serialises an error chain (via errors.Unwrap) into
// Exception values with attached stack traces, in the same format used by
// CaptureError. Useful with CaptureEntry and custom integrations.
func ExceptionsFromError(err error) []Exception {
	if err == nil {
		return nil
	}
	return extractExceptions(err, true)
}

// Flush blocks until all buffered entries are delivered or ctx is cancelled.
// Returns true if all entries were flushed before ctx expired.
func (c *Client) Flush(ctx context.Context) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.transport.Flush(ctx)
}

// Close flushes pending entries and shuts down the transport.
// After Close(), log-level methods silently drop entries and return "".
func (c *Client) Close() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	c.mu.Unlock()

	c.transport.Close()
}

// --- internal ---

func (c *Client) log(ctx context.Context, level Level, message string, metadata map[string]any) EventID {
	c.mu.RLock()
	closed := c.closed
	c.mu.RUnlock()
	if closed {
		return ""
	}

	entry := &LogEntry{
		Level:   level,
		Message: message,
	}
	if len(metadata) > 0 {
		entry.Metadata = metadata
	}

	return c.captureEntry(ctx, entry, nil)
}

// captureEntry runs the full pipeline for a pre-built LogEntry and dispatches it.
// Returns the assigned EventID, or "" if the entry was dropped.
func (c *Client) captureEntry(ctx context.Context, entry *LogEntry, hint *EventHint) EventID {
	// 1. Stamp mandatory fields.
	entry.EventID = newEventID()
	entry.Timestamp = time.Now()
	entry.Service = c.opts.Service
	entry.Release = c.opts.Release
	entry.Environment = c.opts.Environment
	entry.ServerName = c.serverName

	// 2. Enrich with trace context from OTel span or scope.
	traceID, spanID := traceContextFromContext(ctx)
	if entry.TraceID == "" {
		entry.TraceID = traceID
	}
	if entry.SpanID == "" {
		entry.SpanID = spanID
	}

	// 3. Merge active scope.
	if scope := scopeFromContextOrHub(ctx); scope != nil {
		if entry = scope.ApplyToEntry(entry); entry == nil {
			return ""
		}

		// Run scope-level processors.
		// Copy the slice to avoid a data race when AddEventProcessor runs concurrently
		// and append grows into the same backing array.
		scope.mu.RLock()
		procs := make([]EventProcessor, len(scope.eventProcessors))
		copy(procs, scope.eventProcessors)
		scope.mu.RUnlock()
		for _, p := range procs {
			if entry = p(entry, hint); entry == nil {
				return ""
			}
		}
	}

	// 4. Validate.
	if err := validateEntry(entry); err != nil {
		return ""
	}

	// 5. Run client-level processors (integrations).
	// Copy the slice to avoid a data race when AddEventProcessor runs concurrently
	// and append grows into the same backing array.
	c.mu.RLock()
	procs := make([]EventProcessor, len(c.processors))
	copy(procs, c.processors)
	c.mu.RUnlock()
	for _, p := range procs {
		if entry = p(entry, hint); entry == nil {
			return ""
		}
	}

	// 6. Apply BeforeSend hook.
	if c.opts.BeforeSend != nil {
		if entry = c.opts.BeforeSend(entry, hint); entry == nil {
			return ""
		}
	}

	// 7. Sample.
	if c.opts.SampleRate < 1.0 && rand.Float64() > c.opts.SampleRate {
		return ""
	}

	// 8. Dispatch.
	c.transport.Send(entry)
	return entry.EventID
}

// scopeFromContextOrHub resolves the active scope from context or current hub.
func scopeFromContextOrHub(ctx context.Context) *Scope {
	if s := ScopeFromContext(ctx); s != nil {
		return s
	}
	if h := GetHubFromContext(ctx); h != nil {
		return h.Scope()
	}
	return nil
}

// extractExceptions serialises an error chain into a slice of Exception values.
func extractExceptions(err error, attachStack bool) []Exception {
	var result []Exception
	for err != nil {
		ex := Exception{
			Type:  fmt.Sprintf("%T", err),
			Value: err.Error(),
		}
		if attachStack {
			ex.Stacktrace = ExtractStacktrace(err)
		}
		result = append(result, ex)
		err = errors.Unwrap(err)
	}
	return result
}
