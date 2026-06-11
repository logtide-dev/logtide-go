package logtide

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

// Level represents the severity of a log entry.
type Level string

const (
	LevelDebug    Level = "debug"
	LevelInfo     Level = "info"
	LevelWarn     Level = "warn"
	LevelError    Level = "error"
	LevelCritical Level = "critical"
)

// EventID is a unique identifier for a log entry (32 lowercase hex chars, no dashes).
type EventID string

// newEventID generates a random EventID using crypto/rand.
func newEventID() EventID {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return EventID(hex.EncodeToString(b))
}

// LogEntry is the primary unit sent to the LogTide ingest endpoint.
// It is built by the Client and enriched with data from the active Scope.
//
// The wire format is produced by the custom MarshalJSON in wire.go: the
// ingest endpoint only accepts time, service, level, message, metadata,
// trace_id and span_id at the top level, so EventID, Release, Environment,
// ServerName, Tags, Breadcrumbs and Errors are nested inside metadata.
// The json tags below do NOT describe the wire format.
type LogEntry struct {
	EventID     EventID           `json:"event_id"`
	Timestamp   time.Time         `json:"timestamp"`
	Level       Level             `json:"level"`
	Message     string            `json:"message"`
	Service     string            `json:"service"`
	Release     string            `json:"release,omitempty"`
	Environment string            `json:"environment,omitempty"`
	ServerName  string            `json:"server_name,omitempty"`
	Tags        map[string]string `json:"tags,omitempty"`
	Metadata    map[string]any    `json:"metadata,omitempty"`
	Breadcrumbs []*Breadcrumb     `json:"breadcrumbs,omitempty"`
	Errors      []Exception       `json:"errors,omitempty"`
	TraceID     string            `json:"trace_id,omitempty"`
	SpanID      string            `json:"span_id,omitempty"`
}

// EventHint carries supplemental data about the event source.
// Available in BeforeSend hooks.
type EventHint struct {
	// OriginalError is the raw error passed to CaptureError, if applicable.
	OriginalError error
}

// Breadcrumb is a discrete event recorded before a log entry,
// used to reconstruct the execution path leading to an error.
type Breadcrumb struct {
	Type      string         `json:"type,omitempty"`
	Category  string         `json:"category,omitempty"`
	Message   string         `json:"message,omitempty"`
	Data      map[string]any `json:"data,omitempty"`
	Level     Level          `json:"level,omitempty"`
	Timestamp time.Time      `json:"timestamp"`
}

// BreadcrumbHint carries supplemental data for the BeforeBreadcrumb hook.
type BreadcrumbHint map[string]any

// Exception is a serialised Go error with its extracted call stack.
type Exception struct {
	Type       string      `json:"type"`
	Value      string      `json:"value"`
	Stacktrace *Stacktrace `json:"stacktrace,omitempty"`
}

// Stacktrace holds the ordered list of frames for an Exception.
// Frames are ordered outermost-first (deepest caller last).
type Stacktrace struct {
	Frames []Frame `json:"frames"`
}

// Frame is a single Go stack frame.
type Frame struct {
	Function string `json:"function,omitempty"`
	Module   string `json:"module,omitempty"`
	Filename string `json:"filename,omitempty"`
	AbsPath  string `json:"abs_path,omitempty"`
	Lineno   int    `json:"lineno,omitempty"`
	InApp    bool   `json:"in_app"`
}

// User identifies an actor associated with a Scope.
type User struct {
	ID       string `json:"id,omitempty"`
	Email    string `json:"email,omitempty"`
	Username string `json:"username,omitempty"`
	IP       string `json:"ip,omitempty"`
}

// ingestRequest is the wire format sent to the LogTide ingest endpoint.
type ingestRequest struct {
	Logs []LogEntry `json:"logs"`
}

