package logtide_test

import (
	"context"
	"testing"
	"time"

	logtide "github.com/logtide-dev/logtide-sdk-go"
)

func TestScopeSetTag(t *testing.T) {
	s := logtide.NewScope(10)
	s.SetTag("env", "production")

	entry := &logtide.LogEntry{Service: "svc", Level: logtide.LevelInfo, Message: "msg"}
	enriched := s.ApplyToEntry(entry)

	if enriched.Tags["env"] != "production" {
		t.Errorf("tag env = %q, want %q", enriched.Tags["env"], "production")
	}
}

func TestScopeTagPrecedence(t *testing.T) {
	// Scope tags should be overridden by entry-level tags on collision.
	// Per the design: scope tags win over base, entry metadata wins over scope metadata.
	// For Tags: scope wins over entry base, but entry-provided tags override scope.
	s := logtide.NewScope(10)
	s.SetTag("key", "from-scope")

	entry := &logtide.LogEntry{
		Service: "svc",
		Level:   logtide.LevelInfo,
		Message: "msg",
		Tags:    map[string]string{"key": "from-entry"},
	}
	// After ApplyToEntry: scope wins for tags (scope overrides entry base)
	enriched := s.ApplyToEntry(entry)
	// scope overrides entry base tags
	if enriched.Tags["key"] != "from-scope" {
		t.Errorf("tag key = %q, want scope to win (from-scope)", enriched.Tags["key"])
	}
}

func TestScopeClone(t *testing.T) {
	s := logtide.NewScope(10)
	s.SetTag("x", "original")

	clone := s.Clone()
	clone.SetTag("x", "modified")

	// Original should be unchanged.
	entry := &logtide.LogEntry{Service: "svc", Level: logtide.LevelInfo, Message: "msg"}
	original := s.ApplyToEntry(entry)
	if original.Tags["x"] != "original" {
		t.Errorf("original tag x = %q, want original (clone should not affect original)", original.Tags["x"])
	}
}

func TestScopeBreadcrumbs(t *testing.T) {
	s := logtide.NewScope(3)
	for i := 0; i < 5; i++ {
		s.AddBreadcrumb(&logtide.Breadcrumb{
			Message:   "step",
			Timestamp: time.Now(),
		}, nil)
	}

	entry := &logtide.LogEntry{Service: "svc", Level: logtide.LevelInfo, Message: "msg"}
	enriched := s.ApplyToEntry(entry)

	// MaxBreadcrumbs=3 so only last 3 should be kept.
	if len(enriched.Breadcrumbs) != 3 {
		t.Errorf("breadcrumbs = %d, want 3", len(enriched.Breadcrumbs))
	}
}

func TestScopeSetTraceContext(t *testing.T) {
	s := logtide.NewScope(10)
	s.SetTraceContext("aaaaaa11223344556677889900aabbcc", "0011223344556677")

	entry := &logtide.LogEntry{Service: "svc", Level: logtide.LevelInfo, Message: "msg"}
	enriched := s.ApplyToEntry(entry)

	if enriched.TraceID != "aaaaaa11223344556677889900aabbcc" {
		t.Errorf("TraceID = %q", enriched.TraceID)
	}
	if enriched.SpanID != "0011223344556677" {
		t.Errorf("SpanID = %q", enriched.SpanID)
	}
}

func TestScopeDoesNotOverwriteExistingTraceContext(t *testing.T) {
	s := logtide.NewScope(10)
	s.SetTraceContext("scope-trace", "scope-span")

	entry := &logtide.LogEntry{
		Service: "svc", Level: logtide.LevelInfo, Message: "msg",
		TraceID: "entry-trace", SpanID: "entry-span",
	}
	enriched := s.ApplyToEntry(entry)

	if enriched.TraceID != "entry-trace" {
		t.Errorf("TraceID = %q, want entry to win", enriched.TraceID)
	}
}

func TestApplyToEntryNilReturnsNil(t *testing.T) {
	s := logtide.NewScope(10)
	if got := s.ApplyToEntry(nil); got != nil {
		t.Errorf("ApplyToEntry(nil) = %v, want nil", got)
	}
}

func TestAddBreadcrumbDataIsolation(t *testing.T) {
	s := logtide.NewScope(10)
	data := map[string]any{"key": "original"}
	s.AddBreadcrumb(&logtide.Breadcrumb{Message: "b", Data: data}, nil)

	// Mutate the original data map after adding.
	data["key"] = "mutated"

	entry := &logtide.LogEntry{Service: "svc", Level: logtide.LevelInfo, Message: "msg"}
	enriched := s.ApplyToEntry(entry)

	if len(enriched.Breadcrumbs) == 0 {
		t.Fatal("expected breadcrumb")
	}
	if v, ok := enriched.Breadcrumbs[0].Data["key"]; !ok || v != "original" {
		t.Errorf("breadcrumb data[key] = %v, want original (should be isolated copy)", v)
	}
}

func TestScopeTraceContextAccessor(t *testing.T) {
	s := logtide.NewScope(10)
	s.SetTraceContext("trace-aaa", "span-bbb")

	traceID, spanID := s.TraceContext()
	if traceID != "trace-aaa" {
		t.Errorf("traceID = %q, want trace-aaa", traceID)
	}
	if spanID != "span-bbb" {
		t.Errorf("spanID = %q, want span-bbb", spanID)
	}
}

func TestScopeRemoveTag(t *testing.T) {
	s := logtide.NewScope(10)
	s.SetTag("to-remove", "value")
	s.RemoveTag("to-remove")

	entry := &logtide.LogEntry{Service: "svc", Level: logtide.LevelInfo, Message: "msg"}
	enriched := s.ApplyToEntry(entry)
	if _, ok := enriched.Tags["to-remove"]; ok {
		t.Error("removed tag should not appear in entry")
	}
}

func TestScopeClearBreadcrumbs(t *testing.T) {
	s := logtide.NewScope(10)
	s.AddBreadcrumb(&logtide.Breadcrumb{Message: "one", Timestamp: time.Now()}, nil)
	s.ClearBreadcrumbs()

	entry := &logtide.LogEntry{Service: "svc", Level: logtide.LevelInfo, Message: "msg"}
	enriched := s.ApplyToEntry(entry)
	if len(enriched.Breadcrumbs) != 0 {
		t.Errorf("expected no breadcrumbs after clear, got %d", len(enriched.Breadcrumbs))
	}
}

func TestScopeUserInMetadata(t *testing.T) {
	s := logtide.NewScope(10)
	s.SetUser(logtide.User{ID: "u1", Email: "test@example.com"})

	entry := &logtide.LogEntry{Service: "svc", Level: logtide.LevelInfo, Message: "msg"}
	enriched := s.ApplyToEntry(entry)

	if enriched.Metadata == nil {
		t.Fatal("expected metadata to be set")
	}
	if _, ok := enriched.Metadata["user"]; !ok {
		t.Error("expected user key in metadata")
	}
}

func TestScopeUserDoesNotOverwriteExistingMetadataUser(t *testing.T) {
	s := logtide.NewScope(10)
	s.SetUser(logtide.User{ID: "scope-user"})

	entry := &logtide.LogEntry{
		Service:  "svc",
		Level:    logtide.LevelInfo,
		Message:  "msg",
		Metadata: map[string]any{"user": "entry-user"},
	}
	enriched := s.ApplyToEntry(entry)

	// Entry-level "user" should win over scope user.
	if v, _ := enriched.Metadata["user"].(string); v != "entry-user" {
		t.Errorf("metadata[user] = %v, want entry-user", enriched.Metadata["user"])
	}
}

func TestScopeUserDoesNotMutateEntryMetadata(t *testing.T) {
	// Regression test: ApplyToEntry must not mutate the caller's Metadata map
	// when the scope has a user but no scope-level metadata.
	s := logtide.NewScope(10)
	s.SetUser(logtide.User{ID: "scope-user"})

	original := map[string]any{"request_id": "abc-123"}
	entry := &logtide.LogEntry{
		Service:  "svc",
		Level:    logtide.LevelInfo,
		Message:  "msg",
		Metadata: original,
	}
	s.ApplyToEntry(entry)

	// original map must not have been mutated.
	if _, found := original["user"]; found {
		t.Error("ApplyToEntry mutated the caller's Metadata map by adding a 'user' key")
	}
}

func TestScopeAddEventProcessor(t *testing.T) {
	// Processors are run by captureEntry (not ApplyToEntry directly).
	// Verify via the full client pipeline with a scope injected into context.
	c, err := logtide.NewClient(logtide.ClientOptions{
		Service:   "test",
		Transport: logtide.NoopTransport{},
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer c.Close()

	s := logtide.NewScope(10)
	called := false
	s.AddEventProcessor(func(entry *logtide.LogEntry, _ *logtide.EventHint) *logtide.LogEntry {
		called = true
		return entry
	})

	ctx := logtide.WithScope(context.Background(), s)
	c.Info(ctx, "test message", nil)
	if !called {
		t.Error("event processor was not called")
	}

	// Clone should carry the processor too.
	called = false
	clone := s.Clone()
	if clone == nil {
		t.Fatal("clone should not be nil")
	}
	ctx2 := logtide.WithScope(context.Background(), clone)
	c.Info(ctx2, "test message", nil)
	if !called {
		t.Error("cloned scope should carry the event processor")
	}
}
