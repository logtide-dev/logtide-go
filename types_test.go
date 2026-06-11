package logtide_test

import (
	"encoding/json"
	"testing"
	"time"

	logtide "github.com/logtide-dev/logtide-sdk-go"
)

// The LogTide ingest endpoint validates each log against a strict schema
// (time, service, level, message, metadata, trace_id, span_id, session_id)
// and silently drops unknown top-level fields. These tests pin the wire
// format so SDK-only fields are nested inside metadata and survive ingestion.

func fullEntry() logtide.LogEntry {
	return logtide.LogEntry{
		EventID:     "0123456789abcdef0123456789abcdef",
		Timestamp:   time.Date(2026, 6, 11, 10, 30, 0, 0, time.UTC),
		Level:       logtide.LevelError,
		Message:     "boom",
		Service:     "checkout",
		Release:     "1.2.3",
		Environment: "production",
		ServerName:  "web-1",
		Tags:        map[string]string{"region": "eu-west-1"},
		Metadata:    map[string]any{"order_id": "42"},
		Breadcrumbs: []*logtide.Breadcrumb{
			{Type: "http", Category: "request", Message: "GET /cart", Timestamp: time.Date(2026, 6, 11, 10, 29, 59, 0, time.UTC)},
		},
		Errors: []logtide.Exception{
			{
				Type:  "*errors.errorString",
				Value: "boom",
				Stacktrace: &logtide.Stacktrace{Frames: []logtide.Frame{
					{Function: "main", Module: "main", Filename: "main.go", AbsPath: "/app/main.go", Lineno: 12, InApp: true},
				}},
			},
			{Type: "*fmt.wrapError", Value: "cause: io error"},
		},
		TraceID: "4bf92f3577b34da6a3ce929d0e0e4736",
		SpanID:  "00f067aa0ba902b7",
	}
}

func TestLogEntryMarshalWireFormat(t *testing.T) {
	data, err := json.Marshal(fullEntry())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var wire map[string]any
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatalf("unmarshal into map: %v", err)
	}

	// `time` is the field the backend validates; `timestamp` is dropped server-side.
	if wire["time"] != "2026-06-11T10:30:00Z" {
		t.Errorf("time = %v, want 2026-06-11T10:30:00Z", wire["time"])
	}
	for _, forbidden := range []string{"timestamp", "event_id", "tags", "breadcrumbs", "errors", "release", "environment", "server_name"} {
		if _, ok := wire[forbidden]; ok {
			t.Errorf("top-level field %q must not appear in wire format (backend drops it)", forbidden)
		}
	}
	if wire["service"] != "checkout" || wire["level"] != "error" || wire["message"] != "boom" {
		t.Errorf("core fields wrong: %v", wire)
	}
	if wire["trace_id"] != "4bf92f3577b34da6a3ce929d0e0e4736" || wire["span_id"] != "00f067aa0ba902b7" {
		t.Errorf("trace context wrong: %v", wire)
	}

	md, ok := wire["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("metadata missing or not an object: %v", wire["metadata"])
	}
	if md["order_id"] != "42" {
		t.Errorf("user metadata lost: %v", md)
	}
	if md["event_id"] != "0123456789abcdef0123456789abcdef" {
		t.Errorf("metadata.event_id = %v", md["event_id"])
	}
	if md["release"] != "1.2.3" || md["environment"] != "production" || md["server_name"] != "web-1" {
		t.Errorf("release/environment/server_name not nested in metadata: %v", md)
	}
	tags, _ := md["tags"].(map[string]any)
	if tags["region"] != "eu-west-1" {
		t.Errorf("metadata.tags = %v", md["tags"])
	}
	crumbs, _ := md["breadcrumbs"].([]any)
	if len(crumbs) != 1 {
		t.Fatalf("metadata.breadcrumbs = %v", md["breadcrumbs"])
	}

	// Exception chain must follow the backend's StructuredException contract:
	// { type, message, language, stacktrace: [{file, function, line}], cause }
	exc, ok := md["exception"].(map[string]any)
	if !ok {
		t.Fatalf("metadata.exception missing: %v", md)
	}
	if exc["type"] != "*errors.errorString" || exc["message"] != "boom" {
		t.Errorf("exception type/message = %v / %v", exc["type"], exc["message"])
	}
	if exc["language"] != "go" {
		t.Errorf("exception.language = %v, want go", exc["language"])
	}
	frames, _ := exc["stacktrace"].([]any)
	if len(frames) != 1 {
		t.Fatalf("exception.stacktrace = %v", exc["stacktrace"])
	}
	frame, _ := frames[0].(map[string]any)
	if frame["file"] != "/app/main.go" {
		t.Errorf("frame.file = %v, want /app/main.go", frame["file"])
	}
	if frame["function"] != "main.main" {
		t.Errorf("frame.function = %v, want main.main", frame["function"])
	}
	if frame["line"] != float64(12) {
		t.Errorf("frame.line = %v, want 12", frame["line"])
	}
	cause, ok := exc["cause"].(map[string]any)
	if !ok {
		t.Fatalf("exception.cause missing: %v", exc)
	}
	if cause["type"] != "*fmt.wrapError" || cause["message"] != "cause: io error" {
		t.Errorf("cause = %v", cause)
	}
}

func TestLogEntryMarshalMinimal(t *testing.T) {
	entry := logtide.LogEntry{
		Level:   logtide.LevelInfo,
		Message: "hello",
		Service: "api",
	}
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var wire map[string]any
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// Zero timestamp must be omitted so the backend defaults to ingestion time.
	if _, ok := wire["time"]; ok {
		t.Errorf("zero timestamp must be omitted, got %v", wire["time"])
	}
	if _, ok := wire["metadata"]; ok {
		t.Errorf("empty metadata must be omitted, got %v", wire["metadata"])
	}
}

func TestLogEntryJSONRoundTrip(t *testing.T) {
	orig := fullEntry()
	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var back logtide.LogEntry
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if back.EventID != orig.EventID {
		t.Errorf("EventID = %q, want %q", back.EventID, orig.EventID)
	}
	if !back.Timestamp.Equal(orig.Timestamp) {
		t.Errorf("Timestamp = %v, want %v", back.Timestamp, orig.Timestamp)
	}
	if back.Release != orig.Release || back.Environment != orig.Environment || back.ServerName != orig.ServerName {
		t.Errorf("release/env/server = %q/%q/%q", back.Release, back.Environment, back.ServerName)
	}
	if back.Tags["region"] != "eu-west-1" {
		t.Errorf("Tags = %v", back.Tags)
	}
	if len(back.Breadcrumbs) != 1 || back.Breadcrumbs[0].Message != "GET /cart" {
		t.Errorf("Breadcrumbs = %v", back.Breadcrumbs)
	}
	if len(back.Errors) != 2 {
		t.Fatalf("Errors = %v", back.Errors)
	}
	if back.Errors[0].Type != "*errors.errorString" || back.Errors[1].Value != "cause: io error" {
		t.Errorf("Errors chain = %+v", back.Errors)
	}
	if back.Errors[0].Stacktrace == nil || len(back.Errors[0].Stacktrace.Frames) != 1 {
		t.Fatalf("Stacktrace = %+v", back.Errors[0].Stacktrace)
	}
	if back.Errors[0].Stacktrace.Frames[0].Lineno != 12 {
		t.Errorf("frame line = %d", back.Errors[0].Stacktrace.Frames[0].Lineno)
	}
	// SDK-managed keys must not leak back as plain metadata.
	for _, k := range []string{"event_id", "tags", "breadcrumbs", "exception", "release", "environment", "server_name"} {
		if _, ok := back.Metadata[k]; ok {
			t.Errorf("metadata key %q should have been lifted back into the struct", k)
		}
	}
	if back.Metadata["order_id"] != "42" {
		t.Errorf("Metadata = %v", back.Metadata)
	}
}
