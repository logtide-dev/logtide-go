package logtide

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/logtide-dev/logtide-sdk-go/internal/batch"
	"github.com/logtide-dev/logtide-sdk-go/internal/circuitbreaker"
	"github.com/logtide-dev/logtide-sdk-go/internal/httpclient"
	"github.com/logtide-dev/logtide-sdk-go/internal/retry"
)

// drainAndClose drains and closes a response body so the underlying TCP
// connection is returned to the pool and can be reused.
func drainAndClose(body io.ReadCloser) {
	io.Copy(io.Discard, body) //nolint:errcheck
	body.Close()              //nolint:errcheck
}

// Transport dispatches log entries to a backend.
// Implementations must be safe for concurrent use.
type Transport interface {
	// Send enqueues entry for delivery. It must not block the caller.
	Send(entry *LogEntry)

	// Flush blocks until all buffered entries are delivered or ctx is cancelled.
	// Returns true if all entries were flushed before ctx expired.
	Flush(ctx context.Context) bool

	// Close flushes and releases any resources. Safe to call multiple times.
	Close()
}

// NoopTransport discards all log entries. Useful in tests.
type NoopTransport struct{}

func (NoopTransport) Send(*LogEntry)              {}
func (NoopTransport) Flush(context.Context) bool  { return true }
func (NoopTransport) Close()                      {}

// HTTPTransport is the default Transport. It buffers entries via an internal
// batch engine and delivers them in batches to the LogTide ingest endpoint
// using exponential-backoff retry and a circuit breaker.
type HTTPTransport struct {
	batch *batch.Batch[LogEntry]
	debug io.Writer
}

// newHTTPTransport constructs an HTTPTransport from the resolved DSN and options.
// It is called by NewClient.
func newHTTPTransport(dsn *DSN, opts ClientOptions) *HTTPTransport {
	retryConfig := retry.Config{
		MaxRetries: opts.MaxRetries,
		MinBackoff: opts.RetryMinBackoff,
		MaxBackoff: opts.RetryMaxBackoff,
	}

	cb := circuitbreaker.New(opts.CircuitBreakerThreshold, opts.CircuitBreakerTimeout)

	hcOpts := httpclient.Options{
		Timeout: opts.FlushTimeout,
		Version: sdkVersion,
		Inner:   opts.HTTPClient, // nil means build a default client
	}
	hc := httpclient.New(dsn.APIKey, hcOpts)

	ingestURL := dsn.IngestURL()
	debugWriter := opts.DebugWriter

	flushFn := func(ctx context.Context, entries []LogEntry) error {
		if err := cb.Allow(); err != nil {
			logDebug(debugWriter, "circuit breaker open, dropping %d entries", len(entries))
			return ErrCircuitOpen
		}

		req := ingestRequest{Logs: entries}
		resp, err := retry.Do(ctx, retryConfig, func(ctx context.Context) (*http.Response, error) {
			return hc.Post(ctx, ingestURL, req)
		})

		if err != nil {
			cb.RecordFailure()
			logDebug(debugWriter, "send failed: %v", err)
			return fmt.Errorf("logtide: send batch: %w", err)
		}
		if resp == nil {
			cb.RecordFailure()
			return fmt.Errorf("logtide: send batch: nil response")
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			cb.RecordFailure()
			body, _ := httpclient.ReadBody(resp)
			httpErr := &HTTPError{
				StatusCode: resp.StatusCode,
				Message:    fmt.Sprintf("unexpected status %d", resp.StatusCode),
				Body:       body,
			}
			logDebug(debugWriter, "ingest error: %v", httpErr)
			return httpErr
		}

		cb.RecordSuccess()
		drainAndClose(resp.Body)
		return nil
	}

	b := batch.New(batch.Options{
		MaxSize:       opts.BatchSize,
		FlushInterval: opts.FlushInterval,
		FlushTimeout:  opts.FlushTimeout,
	}, flushFn)

	return &HTTPTransport{
		batch: b,
		debug: debugWriter,
	}
}

// Send implements Transport. It is non-blocking.
func (t *HTTPTransport) Send(entry *LogEntry) {
	if err := t.batch.Add(*entry); err != nil {
		logDebug(t.debug, "dropped entry (batch stopped): %v", err)
	}
}

// Flush implements Transport.
func (t *HTTPTransport) Flush(ctx context.Context) bool {
	return t.batch.Flush(ctx) == nil
}

// Close implements Transport.
func (t *HTTPTransport) Close() {
	_ = t.batch.Stop()
}

func logDebug(w io.Writer, format string, args ...any) {
	if w == nil {
		return
	}
	_, _ = fmt.Fprintf(w, "[logtide] "+format+"\n", args...)
}
