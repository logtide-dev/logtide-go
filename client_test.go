package logtide_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	logtide "github.com/logtide-dev/logtide-sdk-go"
)

func newTestServer(t *testing.T, handler func(w http.ResponseWriter, r *http.Request)) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(handler))
	t.Cleanup(srv.Close)
	return srv
}

func newTestDSN(serverURL string) string {
	// Replace https with http and inject a fake API key.
	return "http://lp_testkey@" + serverURL[len("http://"):]
}

// TestClientLogMethodsReturnEventID verifies that all five log methods return EventID.
func TestClientLogMethodsReturnEventID(t *testing.T) {
	client, err := logtide.NewClient(logtide.ClientOptions{
		Service:   "svc",
		Transport: logtide.NoopTransport{},
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()

	ctx := context.Background()
	funcs := []struct {
		name string
		fn   func() logtide.EventID
	}{
		{"Debug", func() logtide.EventID { return client.Debug(ctx, "msg", nil) }},
		{"Info", func() logtide.EventID { return client.Info(ctx, "msg", nil) }},
		{"Warn", func() logtide.EventID { return client.Warn(ctx, "msg", nil) }},
		{"Error", func() logtide.EventID { return client.Error(ctx, "msg", nil) }},
		{"Critical", func() logtide.EventID { return client.Critical(ctx, "msg", nil) }},
	}
	for _, f := range funcs {
		if id := f.fn(); id == "" {
			t.Errorf("%s() returned empty EventID, want non-empty", f.name)
		}
	}
}

// TestClientAttachStacktraceDefault verifies that AttachStacktrace is true by default.
func TestClientAttachStacktraceDefault(t *testing.T) {
	var captured []logtide.LogEntry
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Logs []logtide.LogEntry `json:"logs"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		captured = append(captured, req.Logs...)
		w.WriteHeader(http.StatusOK)
	})

	// Use zero-value ClientOptions (not NewClientOptions) to trigger the default.
	client, err := logtide.NewClient(logtide.ClientOptions{
		DSN:           newTestDSN(srv.URL),
		Service:       "test",
		BatchSize:     10,
		FlushInterval: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	client.CaptureError(ctx, context.DeadlineExceeded, nil)
	client.Flush(ctx)
	time.Sleep(50 * time.Millisecond)
	client.Close()

	if len(captured) == 0 {
		t.Fatal("no entries received")
	}
	if len(captured[0].Errors) == 0 {
		t.Fatal("expected Errors field to be populated by CaptureError")
	}
	if captured[0].Errors[0].Stacktrace == nil {
		t.Error("AttachStacktrace should be true by default — stacktrace should be attached")
	}
}

// TestNewClientValidation checks that NewClient enforces required fields.
func TestNewClientValidation(t *testing.T) {
	t.Run("missing service", func(t *testing.T) {
		_, err := logtide.NewClient(logtide.ClientOptions{DSN: "https://key@api.logtide.dev"})
		if err == nil {
			t.Fatal("expected error for missing service")
		}
	})

	t.Run("missing DSN without custom transport", func(t *testing.T) {
		_, err := logtide.NewClient(logtide.ClientOptions{Service: "svc"})
		if err == nil {
			t.Fatal("expected error for missing DSN")
		}
	})

	t.Run("invalid DSN", func(t *testing.T) {
		_, err := logtide.NewClient(logtide.ClientOptions{Service: "svc", DSN: "not-a-dsn"})
		if err == nil {
			t.Fatal("expected error for invalid DSN")
		}
	})

	t.Run("valid with NoopTransport", func(t *testing.T) {
		client, err := logtide.NewClient(logtide.ClientOptions{
			Service:   "svc",
			Transport: logtide.NoopTransport{},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer client.Close()
	})
}

// TestClientLeveledLogging sends all five log levels and verifies delivery.
func TestClientLeveledLogging(t *testing.T) {
	var received int32
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Logs []logtide.LogEntry `json:"logs"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		atomic.AddInt32(&received, int32(len(req.Logs)))
		json.NewEncoder(w).Encode(map[string]any{"received": len(req.Logs)})
	})

	client, err := logtide.NewClient(logtide.ClientOptions{
		DSN:           newTestDSN(srv.URL),
		Service:       "test-service",
		BatchSize:     10,
		FlushInterval: 100 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()

	ctx := context.Background()
	client.Debug(ctx, "debug msg", nil)
	client.Info(ctx, "info msg", nil)
	client.Warn(ctx, "warn msg", nil)
	client.Error(ctx, "error msg", nil)
	client.Critical(ctx, "critical msg", nil)

	ctx2, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	client.Flush(ctx2)
	time.Sleep(50 * time.Millisecond)

	if n := atomic.LoadInt32(&received); n != 5 {
		t.Errorf("received %d logs, want 5", n)
	}
}

// TestClientBatching verifies that logs are grouped into batches.
func TestClientBatching(t *testing.T) {
	var requests int32
	var totalLogs int32

	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		var req struct {
			Logs []logtide.LogEntry `json:"logs"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		atomic.AddInt32(&totalLogs, int32(len(req.Logs)))
		json.NewEncoder(w).Encode(map[string]any{"received": len(req.Logs)})
	})

	client, err := logtide.NewClient(logtide.ClientOptions{
		DSN:           newTestDSN(srv.URL),
		Service:       "test-service",
		BatchSize:     3,
		FlushInterval: time.Minute,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	ctx := context.Background()
	for i := 0; i < 10; i++ {
		client.Info(ctx, "test", nil)
	}

	time.Sleep(200 * time.Millisecond)
	client.Close()
	time.Sleep(100 * time.Millisecond)

	if n := atomic.LoadInt32(&totalLogs); n != 10 {
		t.Errorf("total logs = %d, want 10", n)
	}
	if r := atomic.LoadInt32(&requests); r < 1 {
		t.Errorf("requests = %d, want >= 1", r)
	}
}

// TestClientCloseFlushes verifies that Close delivers buffered logs.
func TestClientCloseFlushes(t *testing.T) {
	var received int32
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Logs []logtide.LogEntry `json:"logs"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		atomic.AddInt32(&received, int32(len(req.Logs)))
		json.NewEncoder(w).Encode(map[string]any{"received": len(req.Logs)})
	})

	client, err := logtide.NewClient(logtide.ClientOptions{
		DSN:           newTestDSN(srv.URL),
		Service:       "test-service",
		BatchSize:     100,
		FlushInterval: time.Minute,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	ctx := context.Background()
	for i := 0; i < 10; i++ {
		client.Info(ctx, "test", nil)
	}
	client.Close()
	time.Sleep(100 * time.Millisecond)

	if n := atomic.LoadInt32(&received); n != 10 {
		t.Errorf("received %d, want 10", n)
	}

	// Log after close silently drops the entry and returns empty EventID.
	if id := client.Info(ctx, "after close", nil); id != "" {
		t.Errorf("Info after close = %q, want empty EventID", id)
	}
}

// TestClientBeforeSend verifies the BeforeSend hook can drop entries.
func TestClientBeforeSend(t *testing.T) {
	var received int32
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Logs []logtide.LogEntry `json:"logs"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		atomic.AddInt32(&received, int32(len(req.Logs)))
		json.NewEncoder(w).Encode(map[string]any{"received": len(req.Logs)})
	})

	client, err := logtide.NewClient(logtide.ClientOptions{
		DSN:     newTestDSN(srv.URL),
		Service: "test-service",
		BeforeSend: func(entry *logtide.LogEntry, _ *logtide.EventHint) *logtide.LogEntry {
			// Drop debug entries.
			if entry.Level == logtide.LevelDebug {
				return nil
			}
			return entry
		},
		BatchSize:     10,
		FlushInterval: 100 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()

	ctx := context.Background()
	client.Debug(ctx, "dropped", nil)
	client.Info(ctx, "kept", nil)

	ctx2, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	client.Flush(ctx2)
	time.Sleep(50 * time.Millisecond)

	if n := atomic.LoadInt32(&received); n != 1 {
		t.Errorf("received %d, want 1 (debug should be dropped)", n)
	}
}

// TestClientCaptureError verifies error serialisation.
func TestClientCaptureError(t *testing.T) {
	var captured []logtide.LogEntry
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Logs []logtide.LogEntry `json:"logs"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		captured = append(captured, req.Logs...)
		json.NewEncoder(w).Encode(map[string]any{"received": len(req.Logs)})
	})

	client, err := logtide.NewClient(logtide.ClientOptions{
		DSN:           newTestDSN(srv.URL),
		Service:       "test-service",
		BatchSize:     10,
		FlushInterval: 100 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	id := client.CaptureError(ctx, context.DeadlineExceeded, nil)
	if id == "" {
		t.Fatal("CaptureError returned empty EventID")
	}

	client.Flush(ctx)
	time.Sleep(50 * time.Millisecond)

	if len(captured) == 0 {
		t.Fatal("no log entries received")
	}
	if captured[0].Level != logtide.LevelError {
		t.Errorf("level = %v, want error", captured[0].Level)
	}
}

// TestClientScopeEnrichment verifies scope tags are merged into entries.
func TestClientScopeEnrichment(t *testing.T) {
	var captured []logtide.LogEntry
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Logs []logtide.LogEntry `json:"logs"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		captured = append(captured, req.Logs...)
		json.NewEncoder(w).Encode(map[string]any{"received": len(req.Logs)})
	})

	client, err := logtide.NewClient(logtide.ClientOptions{
		DSN:           newTestDSN(srv.URL),
		Service:       "test-service",
		BatchSize:     10,
		FlushInterval: 100 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()

	scope := logtide.NewScope(10)
	scope.SetTag("request_id", "abc-123")
	ctx := logtide.WithScope(context.Background(), scope)

	client.Info(ctx, "with scope", nil)

	flushCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	client.Flush(flushCtx)
	time.Sleep(50 * time.Millisecond)

	if len(captured) == 0 {
		t.Fatal("no entries received")
	}
	if captured[0].Tags["request_id"] != "abc-123" {
		t.Errorf("tag request_id = %q, want %q", captured[0].Tags["request_id"], "abc-123")
	}
}
