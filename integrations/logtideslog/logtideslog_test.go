package logtideslog_test

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"

	logtide "github.com/logtide-dev/logtide-sdk-go"
	"github.com/logtide-dev/logtide-sdk-go/integrations/logtideslog"
)

// captureTransport records every entry it receives.
type captureTransport struct {
	mu      sync.Mutex
	entries []*logtide.LogEntry
}

func (t *captureTransport) Send(entry *logtide.LogEntry) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.entries = append(t.entries, entry)
}
func (t *captureTransport) Flush(context.Context) bool { return true }
func (t *captureTransport) Close()                     {}

func (t *captureTransport) all() []*logtide.LogEntry {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]*logtide.LogEntry, len(t.entries))
	copy(out, t.entries)
	return out
}

func newTestLogger(t *testing.T, opts *logtideslog.Options) (*slog.Logger, *captureTransport) {
	t.Helper()
	transport := &captureTransport{}
	client, err := logtide.NewClient(logtide.ClientOptions{
		Service:   "slog-test",
		Transport: transport,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(client.Close)
	return slog.New(logtideslog.New(client, opts)), transport
}

func TestHandlerRoutesRecords(t *testing.T) {
	logger, transport := newTestLogger(t, nil)

	logger.Info("user logged in", "user_id", 42, "plan", "pro")

	entries := transport.all()
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	e := entries[0]
	if e.Level != logtide.LevelInfo {
		t.Errorf("level = %s, want info", e.Level)
	}
	if e.Message != "user logged in" {
		t.Errorf("message = %q", e.Message)
	}
	if e.Service != "slog-test" {
		t.Errorf("service = %q", e.Service)
	}
	if e.Metadata["user_id"] != int64(42) {
		t.Errorf("metadata.user_id = %v (%T)", e.Metadata["user_id"], e.Metadata["user_id"])
	}
	if e.Metadata["plan"] != "pro" {
		t.Errorf("metadata.plan = %v", e.Metadata["plan"])
	}
}

func TestHandlerLevelMapping(t *testing.T) {
	logger, transport := newTestLogger(t, &logtideslog.Options{Level: slog.LevelDebug})

	logger.Debug("d")
	logger.Info("i")
	logger.Warn("w")
	logger.Error("e")
	logger.Log(context.Background(), slog.LevelError+4, "c")

	entries := transport.all()
	if len(entries) != 5 {
		t.Fatalf("entries = %d, want 5", len(entries))
	}
	want := []logtide.Level{
		logtide.LevelDebug, logtide.LevelInfo, logtide.LevelWarn,
		logtide.LevelError, logtide.LevelCritical,
	}
	for i, lvl := range want {
		if entries[i].Level != lvl {
			t.Errorf("entries[%d].Level = %s, want %s", i, entries[i].Level, lvl)
		}
	}
}

func TestHandlerRespectsMinLevel(t *testing.T) {
	logger, transport := newTestLogger(t, &logtideslog.Options{Level: slog.LevelWarn})

	logger.Debug("nope")
	logger.Info("nope")
	logger.Warn("yes")

	entries := transport.all()
	if len(entries) != 1 || entries[0].Message != "yes" {
		t.Fatalf("entries = %+v, want only the warn record", entries)
	}
}

func TestHandlerGroupsAndAttrs(t *testing.T) {
	logger, transport := newTestLogger(t, nil)

	logger.With("request_id", "abc").WithGroup("http").Info("done", "status", 200)

	entries := transport.all()
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	md := entries[0].Metadata
	if md["request_id"] != "abc" {
		t.Errorf("metadata.request_id = %v", md["request_id"])
	}
	group, ok := md["http"].(map[string]any)
	if !ok {
		t.Fatalf("metadata.http = %v (%T), want group map", md["http"], md["http"])
	}
	if group["status"] != int64(200) {
		t.Errorf("metadata.http.status = %v (%T)", group["status"], group["status"])
	}
}

func TestHandlerAttachesErrorsAsExceptions(t *testing.T) {
	logger, transport := newTestLogger(t, nil)

	wrapped := errors.New("connection refused")
	logger.Error("db query failed", "error", wrapped)

	entries := transport.all()
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	e := entries[0]
	if len(e.Errors) == 0 {
		t.Fatalf("entry.Errors empty; error attr should become a structured exception")
	}
	if e.Errors[0].Value != "connection refused" {
		t.Errorf("exception value = %q", e.Errors[0].Value)
	}
	// the raw error must not stay in metadata once promoted
	if _, ok := e.Metadata["error"]; ok {
		t.Errorf("metadata.error should have been promoted to entry.Errors")
	}
}

func TestCaptureEntryPublicAPI(t *testing.T) {
	transport := &captureTransport{}
	client, err := logtide.NewClient(logtide.ClientOptions{
		Service:   "api-test",
		Transport: transport,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()

	id := client.CaptureEntry(context.Background(), &logtide.LogEntry{
		Level:   logtide.LevelWarn,
		Message: "custom entry",
		Errors:  logtide.ExceptionsFromError(errors.New("boom")),
	})
	if id == "" {
		t.Fatal("CaptureEntry returned empty EventID")
	}
	entries := transport.all()
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	if entries[0].Service != "api-test" {
		t.Errorf("service not stamped: %q", entries[0].Service)
	}
	if len(entries[0].Errors) != 1 || entries[0].Errors[0].Value != "boom" {
		t.Errorf("errors = %+v", entries[0].Errors)
	}
}
