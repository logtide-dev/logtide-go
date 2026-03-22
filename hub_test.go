package logtide_test

import (
	"context"
	"encoding/json"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	logtide "github.com/logtide-dev/logtide-sdk-go"
)

// newNoopClient returns a Client backed by NoopTransport for hub tests.
func newNoopClient(t *testing.T) *logtide.Client {
	t.Helper()
	c, err := logtide.NewClient(logtide.ClientOptions{
		Service:   "test",
		Transport: logtide.NoopTransport{},
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(c.Close)
	return c
}

func TestHubClientAndScope(t *testing.T) {
	client := newNoopClient(t)
	scope := logtide.NewScope(10)
	hub := logtide.NewHub(client, scope)

	if hub.Client() != client {
		t.Error("Client() should return the bound client")
	}
	if hub.Scope() != scope {
		t.Error("Scope() should return the bound scope")
	}
}

func TestHubNilScopeCreatedAutomatically(t *testing.T) {
	client := newNoopClient(t)
	hub := logtide.NewHub(client, nil)
	if hub.Scope() == nil {
		t.Error("Scope() should not be nil when nil is passed to NewHub")
	}
}

func TestHubPushPopScope(t *testing.T) {
	hub := logtide.NewHub(newNoopClient(t), nil)
	original := hub.Scope()
	original.SetTag("base", "yes")

	pushed := hub.PushScope()
	pushed.SetTag("pushed", "yes")

	// The pushed scope has the base tag (it's a clone).
	entry := &logtide.LogEntry{Service: "svc", Level: logtide.LevelInfo, Message: "msg"}
	enriched := pushed.ApplyToEntry(entry)
	if enriched.Tags["base"] != "yes" {
		t.Error("pushed scope should inherit base tags")
	}
	if enriched.Tags["pushed"] != "yes" {
		t.Error("pushed scope should have its own tags")
	}

	// After pop, top scope is the original again.
	hub.PopScope()
	if hub.Scope() != original {
		t.Error("after PopScope, should return original scope")
	}
	// Original should not have the "pushed" tag.
	entry2 := &logtide.LogEntry{Service: "svc", Level: logtide.LevelInfo, Message: "msg"}
	e2 := original.ApplyToEntry(entry2)
	if _, ok := e2.Tags["pushed"]; ok {
		t.Error("original scope should not have tag set on pushed scope")
	}
}

func TestHubPopScopeNoop(t *testing.T) {
	// PopScope when stack has only one layer is a no-op.
	hub := logtide.NewHub(newNoopClient(t), nil)
	hub.PopScope()
	hub.PopScope()
	if hub.Scope() == nil {
		t.Error("scope should not be nil after excess PopScope calls")
	}
}

func TestHubWithScope(t *testing.T) {
	hub := logtide.NewHub(newNoopClient(t), nil)
	original := hub.Scope()

	var innerScope *logtide.Scope
	hub.WithScope(func(s *logtide.Scope) {
		innerScope = s
		s.SetTag("inner", "yes")
		// Inside WithScope, top should be the child.
		if hub.Scope() == original {
			t.Error("inside WithScope, scope should be child, not original")
		}
	})

	// After WithScope, top is restored.
	if hub.Scope() != original {
		t.Error("after WithScope, original scope should be restored")
	}
	_ = innerScope
}

func TestHubConfigureScope(t *testing.T) {
	hub := logtide.NewHub(newNoopClient(t), nil)
	hub.ConfigureScope(func(s *logtide.Scope) {
		s.SetTag("configured", "true")
	})
	entry := &logtide.LogEntry{Service: "svc", Level: logtide.LevelInfo, Message: "msg"}
	if v := hub.Scope().ApplyToEntry(entry).Tags["configured"]; v != "true" {
		t.Errorf("tag configured = %q, want true", v)
	}
}

func TestHubBindClient(t *testing.T) {
	hub := logtide.NewHub(nil, nil)
	if hub.Client() != nil {
		t.Error("initial client should be nil")
	}
	client := newNoopClient(t)
	hub.BindClient(client)
	if hub.Client() != client {
		t.Error("BindClient should update the client")
	}
}

func TestHubCloneIsIndependent(t *testing.T) {
	hub := logtide.NewHub(newNoopClient(t), nil)
	hub.ConfigureScope(func(s *logtide.Scope) {
		s.SetTag("original", "yes")
	})

	clone := hub.Clone()
	clone.ConfigureScope(func(s *logtide.Scope) {
		s.SetTag("clone-only", "yes")
	})

	// Original hub should NOT have the clone-only tag.
	entry := &logtide.LogEntry{Service: "svc", Level: logtide.LevelInfo, Message: "msg"}
	orig := hub.Scope().ApplyToEntry(entry)
	if _, ok := orig.Tags["clone-only"]; ok {
		t.Error("original hub scope should not have tag set on clone")
	}
	// Clone should have both tags.
	cloneEntry := &logtide.LogEntry{Service: "svc", Level: logtide.LevelInfo, Message: "msg"}
	ce := clone.Scope().ApplyToEntry(cloneEntry)
	if ce.Tags["original"] != "yes" {
		t.Error("clone should inherit original tags")
	}
	if ce.Tags["clone-only"] != "yes" {
		t.Error("clone should have its own tags")
	}
}

func TestHubNilClientSilentDrop(t *testing.T) {
	hub := logtide.NewHub(nil, nil)
	ctx := context.Background()

	// All capture methods should return "" without panicking.
	if id := hub.Debug(ctx, "msg", nil); id != "" {
		t.Errorf("Debug with nil client = %q, want \"\"", id)
	}
	if id := hub.Info(ctx, "msg", nil); id != "" {
		t.Errorf("Info with nil client = %q, want \"\"", id)
	}
	if id := hub.Warn(ctx, "msg", nil); id != "" {
		t.Errorf("Warn with nil client = %q, want \"\"", id)
	}
	if id := hub.Error(ctx, "msg", nil); id != "" {
		t.Errorf("Error with nil client = %q, want \"\"", id)
	}
	if id := hub.Critical(ctx, "msg", nil); id != "" {
		t.Errorf("Critical with nil client = %q, want \"\"", id)
	}
	if id := hub.CaptureError(ctx, context.DeadlineExceeded, nil); id != "" {
		t.Errorf("CaptureError with nil client = %q, want \"\"", id)
	}
}

func TestHubFlushNilClientReturnsTrue(t *testing.T) {
	hub := logtide.NewHub(nil, nil)
	if !hub.Flush(time.Second) {
		t.Error("Flush with nil client should return true")
	}
}

func TestHubLastEventID(t *testing.T) {
	var received int32
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&received, 1)
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
	defer client.Close()

	hub := logtide.NewHub(client, nil)
	if hub.LastEventID() != "" {
		t.Error("LastEventID should be empty initially")
	}

	id := hub.Info(context.Background(), "hello", nil)
	if id == "" {
		t.Fatal("Info should return a non-empty EventID")
	}
	if hub.LastEventID() != id {
		t.Errorf("LastEventID = %q, want %q", hub.LastEventID(), id)
	}
}

func TestHubAddBreadcrumb(t *testing.T) {
	hub := logtide.NewHub(newNoopClient(t), nil)
	hub.AddBreadcrumb(&logtide.Breadcrumb{Message: "crumb", Timestamp: time.Now()}, nil)

	entry := &logtide.LogEntry{Service: "svc", Level: logtide.LevelInfo, Message: "msg"}
	enriched := hub.Scope().ApplyToEntry(entry)
	if len(enriched.Breadcrumbs) != 1 {
		t.Errorf("breadcrumbs = %d, want 1", len(enriched.Breadcrumbs))
	}
}

func TestHubScopeEnrichesEntries(t *testing.T) {
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

	hub := logtide.NewHub(client, nil)
	hub.ConfigureScope(func(s *logtide.Scope) {
		s.SetTag("hub-tag", "present")
	})

	hub.Info(context.Background(), "msg", nil)

	flushCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	client.Flush(flushCtx)
	time.Sleep(50 * time.Millisecond)
	client.Close()

	if len(captured) == 0 {
		t.Fatal("no entries received")
	}
	if captured[0].Tags["hub-tag"] != "present" {
		t.Errorf("hub-tag = %q, want present", captured[0].Tags["hub-tag"])
	}
}

func TestPackageLevelInit(t *testing.T) {
	var received int32
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Logs []logtide.LogEntry `json:"logs"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		atomic.AddInt32(&received, int32(len(req.Logs)))
		w.WriteHeader(http.StatusOK)
	})

	flush := logtide.Init(logtide.ClientOptions{
		DSN:           newTestDSN(srv.URL),
		Service:       "pkg-test",
		BatchSize:     10,
		FlushInterval: 50 * time.Millisecond,
	})

	ctx := context.Background()
	logtide.Debug(ctx, "d", nil)
	logtide.Info(ctx, "i", nil)
	logtide.Warn(ctx, "w", nil)
	logtide.Error(ctx, "e", nil)
	logtide.Critical(ctx, "c", nil)

	flush()
	time.Sleep(50 * time.Millisecond)

	if n := atomic.LoadInt32(&received); n != 5 {
		t.Errorf("received %d, want 5", n)
	}
}
