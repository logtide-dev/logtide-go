package logtide

import (
	"encoding/json"
	"strings"
	"time"
)

// The ingest endpoint (POST /api/v1/ingest) validates every log against a
// strict schema accepting only: time, service, level, message, metadata,
// trace_id, span_id, session_id. Unknown top-level fields are silently
// dropped server-side, so SDK-level fields (event_id, tags, breadcrumbs,
// errors, release, environment, server_name) must travel inside metadata.
// Exceptions use the backend's StructuredException contract
// (metadata.exception) so error grouping and fingerprinting work.

// wireLogEntry is the exact top-level shape accepted by the ingest endpoint.
type wireLogEntry struct {
	Time     string         `json:"time,omitempty"`
	Service  string         `json:"service"`
	Level    Level          `json:"level"`
	Message  string         `json:"message"`
	Metadata map[string]any `json:"metadata,omitempty"`
	TraceID  string         `json:"trace_id,omitempty"`
	SpanID   string         `json:"span_id,omitempty"`
}

// structuredException mirrors the backend's StructuredException type.
type structuredException struct {
	Type       string               `json:"type"`
	Message    string               `json:"message"`
	Language   string               `json:"language"`
	Stacktrace []structuredFrame    `json:"stacktrace,omitempty"`
	Cause      *structuredException `json:"cause,omitempty"`
}

// structuredFrame mirrors the backend's StructuredStackFrame type. The
// frame-level metadata object is preserved verbatim by the backend; we use
// it to round-trip Go-specific frame fields (module, relative filename,
// in_app) that have no dedicated slot in the contract.
type structuredFrame struct {
	File     string         `json:"file,omitempty"`
	Function string         `json:"function,omitempty"`
	Line     int            `json:"line,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// MarshalJSON serialises the entry into the ingest wire format.
func (e LogEntry) MarshalJSON() ([]byte, error) {
	w := wireLogEntry{
		Service: e.Service,
		Level:   e.Level,
		Message: e.Message,
		TraceID: e.TraceID,
		SpanID:  e.SpanID,
	}
	// A zero timestamp is omitted so the backend defaults to ingestion time.
	if !e.Timestamp.IsZero() {
		w.Time = e.Timestamp.UTC().Format(time.RFC3339Nano)
	}

	md := make(map[string]any, len(e.Metadata)+7)
	for k, v := range e.Metadata {
		md[k] = v
	}
	if e.EventID != "" {
		md["event_id"] = string(e.EventID)
	}
	if e.Release != "" {
		md["release"] = e.Release
	}
	if e.Environment != "" {
		md["environment"] = e.Environment
	}
	if e.ServerName != "" {
		md["server_name"] = e.ServerName
	}
	if len(e.Tags) > 0 {
		md["tags"] = e.Tags
	}
	if len(e.Breadcrumbs) > 0 {
		md["breadcrumbs"] = e.Breadcrumbs
	}
	if len(e.Errors) > 0 {
		// Caller-provided metadata.exception wins over the SDK chain.
		if _, ok := md["exception"]; !ok {
			md["exception"] = toStructuredException(e.Errors)
		}
	}
	if len(md) > 0 {
		w.Metadata = md
	}

	return json.Marshal(w)
}

// UnmarshalJSON reverses MarshalJSON: well-known SDK keys are lifted out of
// metadata back into their struct fields; everything else stays in Metadata.
func (e *LogEntry) UnmarshalJSON(data []byte) error {
	var w struct {
		Time     string                     `json:"time"`
		Service  string                     `json:"service"`
		Level    Level                      `json:"level"`
		Message  string                     `json:"message"`
		Metadata map[string]json.RawMessage `json:"metadata"`
		TraceID  string                     `json:"trace_id"`
		SpanID   string                     `json:"span_id"`
	}
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}

	*e = LogEntry{
		Service: w.Service,
		Level:   w.Level,
		Message: w.Message,
		TraceID: w.TraceID,
		SpanID:  w.SpanID,
	}
	if w.Time != "" {
		if t, err := time.Parse(time.RFC3339Nano, w.Time); err == nil {
			e.Timestamp = t
		}
	}

	lift := func(key string, dst any) bool {
		raw, ok := w.Metadata[key]
		if !ok {
			return false
		}
		if err := json.Unmarshal(raw, dst); err != nil {
			return false // malformed: leave it in plain metadata
		}
		delete(w.Metadata, key)
		return true
	}

	var eventID string
	if lift("event_id", &eventID) {
		e.EventID = EventID(eventID)
	}
	lift("release", &e.Release)
	lift("environment", &e.Environment)
	lift("server_name", &e.ServerName)
	lift("tags", &e.Tags)
	lift("breadcrumbs", &e.Breadcrumbs)
	var se structuredException
	if lift("exception", &se) {
		e.Errors = fromStructuredException(&se)
	}

	if len(w.Metadata) > 0 {
		e.Metadata = make(map[string]any, len(w.Metadata))
		for k, raw := range w.Metadata {
			var v any
			if err := json.Unmarshal(raw, &v); err != nil {
				continue
			}
			e.Metadata[k] = v
		}
	}

	return nil
}

// toStructuredException converts an error chain (outermost first, as built
// by extractExceptions) into the backend's nested cause representation.
func toStructuredException(chain []Exception) *structuredException {
	var root *structuredException
	for i := len(chain) - 1; i >= 0; i-- {
		ex := chain[i]
		node := &structuredException{
			Type:     ex.Type,
			Message:  ex.Value,
			Language: "go",
			Cause:    root,
		}
		if ex.Stacktrace != nil {
			node.Stacktrace = make([]structuredFrame, 0, len(ex.Stacktrace.Frames))
			for _, f := range ex.Stacktrace.Frames {
				node.Stacktrace = append(node.Stacktrace, toStructuredFrame(f))
			}
		}
		root = node
	}
	return root
}

func toStructuredFrame(f Frame) structuredFrame {
	sf := structuredFrame{
		Function: f.Function,
		Line:     f.Lineno,
	}
	if f.Module != "" {
		sf.Function = f.Module + "." + f.Function
	}
	if f.AbsPath != "" {
		sf.File = f.AbsPath
	} else {
		sf.File = f.Filename
	}

	md := make(map[string]any, 3)
	if f.Module != "" {
		md["module"] = f.Module
	}
	if f.AbsPath != "" && f.Filename != "" && f.Filename != f.AbsPath {
		md["filename"] = f.Filename
	}
	md["in_app"] = f.InApp
	sf.Metadata = md
	return sf
}

// fromStructuredException flattens the nested cause chain back into the
// outermost-first slice used by the SDK.
func fromStructuredException(se *structuredException) []Exception {
	var out []Exception
	for cur := se; cur != nil; cur = cur.Cause {
		ex := Exception{Type: cur.Type, Value: cur.Message}
		if len(cur.Stacktrace) > 0 {
			frames := make([]Frame, 0, len(cur.Stacktrace))
			for _, sf := range cur.Stacktrace {
				frames = append(frames, fromStructuredFrame(sf))
			}
			ex.Stacktrace = &Stacktrace{Frames: frames}
		}
		out = append(out, ex)
	}
	return out
}

func fromStructuredFrame(sf structuredFrame) Frame {
	f := Frame{Lineno: sf.Line, Function: sf.Function}
	if m, ok := sf.Metadata["module"].(string); ok && m != "" {
		f.Module = m
		f.Function = strings.TrimPrefix(sf.Function, m+".")
	}
	if fn, ok := sf.Metadata["filename"].(string); ok && fn != "" {
		f.Filename = fn
		f.AbsPath = sf.File
	} else {
		f.Filename = sf.File
	}
	if ia, ok := sf.Metadata["in_app"].(bool); ok {
		f.InApp = ia
	}
	return f
}
