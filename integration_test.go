package logtide_test

import (
	"context"
	"encoding/json"
	"net/http"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	logtide "github.com/logtide-dev/logtide-sdk-go"
)

func TestEnvironmentIntegrationAddsRuntime(t *testing.T) {
	var captured []logtide.LogEntry
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Logs []logtide.LogEntry `json:"logs"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		captured = append(captured, req.Logs...)
		w.WriteHeader(http.StatusOK)
	})

	client, err := logtide.NewClient(logtide.ClientOptions{
		DSN:           newTestDSN(srv.URL),
		Service:       "test",
		BatchSize:     10,
		FlushInterval: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	client.Info(context.Background(), "hello", nil)

	flushCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	client.Flush(flushCtx)
	time.Sleep(50 * time.Millisecond)
	client.Close()

	if len(captured) == 0 {
		t.Fatal("no entries captured")
	}
	entry := captured[0]
	if entry.Metadata == nil {
		t.Fatal("metadata is nil; EnvironmentIntegration should have set it")
	}
	rtMeta, ok := entry.Metadata["runtime"].(map[string]any)
	if !ok {
		t.Fatalf("metadata[runtime] type = %T, want map[string]any", entry.Metadata["runtime"])
	}
	if rtMeta["go"] != runtime.Version() {
		t.Errorf("runtime.go = %v, want %v", rtMeta["go"], runtime.Version())
	}
	if rtMeta["os"] != runtime.GOOS {
		t.Errorf("runtime.os = %v, want %v", rtMeta["os"], runtime.GOOS)
	}
	if rtMeta["arch"] != runtime.GOARCH {
		t.Errorf("runtime.arch = %v, want %v", rtMeta["arch"], runtime.GOARCH)
	}
}

func TestEnvironmentIntegrationDoesNotOverrideExisting(t *testing.T) {
	var captured []logtide.LogEntry
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Logs []logtide.LogEntry `json:"logs"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		captured = append(captured, req.Logs...)
		w.WriteHeader(http.StatusOK)
	})

	client, err := logtide.NewClient(logtide.ClientOptions{
		DSN:           newTestDSN(srv.URL),
		Service:       "test",
		BatchSize:     10,
		FlushInterval: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	// Entry with pre-set runtime metadata.
	client.Info(context.Background(), "hello", map[string]any{"runtime": "custom"})

	flushCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	client.Flush(flushCtx)
	time.Sleep(50 * time.Millisecond)
	client.Close()

	if len(captured) == 0 {
		t.Fatal("no entries captured")
	}
	// The integration should not overwrite existing "runtime" key.
	if v, _ := captured[0].Metadata["runtime"].(string); v != "custom" {
		t.Errorf("metadata[runtime] = %v, want custom (should not be overwritten)", captured[0].Metadata["runtime"])
	}
}

func TestGlobalTagsIntegrationAppliesTags(t *testing.T) {
	var captured []logtide.LogEntry
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Logs []logtide.LogEntry `json:"logs"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		captured = append(captured, req.Logs...)
		w.WriteHeader(http.StatusOK)
	})

	client, err := logtide.NewClient(logtide.ClientOptions{
		DSN:     newTestDSN(srv.URL),
		Service: "test",
		Tags: map[string]string{
			"region": "eu-west-1",
			"env":    "staging",
		},
		BatchSize:     10,
		FlushInterval: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	client.Info(context.Background(), "hello", nil)

	flushCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	client.Flush(flushCtx)
	time.Sleep(50 * time.Millisecond)
	client.Close()

	if len(captured) == 0 {
		t.Fatal("no entries captured")
	}
	if captured[0].Tags["region"] != "eu-west-1" {
		t.Errorf("tag region = %q, want eu-west-1", captured[0].Tags["region"])
	}
	if captured[0].Tags["env"] != "staging" {
		t.Errorf("tag env = %q, want staging", captured[0].Tags["env"])
	}
}

func TestGlobalTagsScopeTagsWinOnCollision(t *testing.T) {
	// Scope tags are merged into the entry before GlobalTagsIntegration runs.
	// GlobalTagsIntegration uses mergeTags(global, entry.Tags), so entry/scope tags win.
	var captured []logtide.LogEntry
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Logs []logtide.LogEntry `json:"logs"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		captured = append(captured, req.Logs...)
		w.WriteHeader(http.StatusOK)
	})

	client, err := logtide.NewClient(logtide.ClientOptions{
		DSN:           newTestDSN(srv.URL),
		Service:       "test",
		Tags:          map[string]string{"env": "global"},
		BatchSize:     10,
		FlushInterval: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	// Scope sets "env" → it should override the global tag.
	scope := logtide.NewScope(10)
	scope.SetTag("env", "request-scoped")
	ctx := logtide.WithScope(context.Background(), scope)

	client.Info(ctx, "hello", nil)

	flushCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	client.Flush(flushCtx)
	time.Sleep(50 * time.Millisecond)
	client.Close()

	if len(captured) == 0 {
		t.Fatal("no entries captured")
	}
	// Scope tag should win over global tag.
	if captured[0].Tags["env"] != "request-scoped" {
		t.Errorf("tag env = %q, want request-scoped (scope tags should override global)", captured[0].Tags["env"])
	}
}

func TestIntegrationDeduplication(t *testing.T) {
	var received int32
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Logs []logtide.LogEntry `json:"logs"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		atomic.AddInt32(&received, int32(len(req.Logs)))
		w.WriteHeader(http.StatusOK)
	})

	// Register the same integration twice; only one processor should be added.
	var processorCalls int32
	dupIntegration := &duplicateIntegration{calls: &processorCalls}

	client, err := logtide.NewClient(logtide.ClientOptions{
		DSN:     newTestDSN(srv.URL),
		Service: "test",
		Integrations: func(defaults []logtide.Integration) []logtide.Integration {
			return append(defaults, dupIntegration, dupIntegration)
		},
		BatchSize:     10,
		FlushInterval: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	client.Info(context.Background(), "msg", nil)

	flushCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	client.Flush(flushCtx)
	time.Sleep(50 * time.Millisecond)
	client.Close()

	// Processor should have been called exactly once per entry (not twice).
	if n := atomic.LoadInt32(&processorCalls); n != 1 {
		t.Errorf("processor called %d times, want 1 (dedup should prevent double registration)", n)
	}
}

// duplicateIntegration is a test integration that counts processor invocations.
type duplicateIntegration struct {
	calls *int32
}

func (d *duplicateIntegration) Name() string { return "DuplicateTest" }

func (d *duplicateIntegration) Setup(client *logtide.Client) {
	client.AddEventProcessor(func(e *logtide.LogEntry, _ *logtide.EventHint) *logtide.LogEntry {
		atomic.AddInt32(d.calls, 1)
		return e
	})
}
