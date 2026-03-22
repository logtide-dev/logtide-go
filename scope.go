package logtide

import (
	"context"
	"sync"
	"time"
)

type contextKey int

const (
	scopeContextKey contextKey = iota + 1
	hubContextKey
)

// Scope carries per-request contextual data that is merged into every log
// entry produced while the scope is active.
//
// Scope is safe for concurrent use. Use Clone to produce an independent copy
// for per-request isolation.
type Scope struct {
	mu              sync.RWMutex
	tags            map[string]string
	metadata        map[string]any
	user            User
	breadcrumbs     []*Breadcrumb
	maxBreadcrumbs  int
	traceID         string
	spanID          string
	eventProcessors []EventProcessor
}

// EventProcessor is a function that may inspect or mutate a LogEntry before
// it is dispatched. Returning nil drops the entry.
type EventProcessor func(entry *LogEntry, hint *EventHint) *LogEntry

// NewScope creates an empty Scope with the given breadcrumb capacity.
func NewScope(maxBreadcrumbs int) *Scope {
	if maxBreadcrumbs <= 0 {
		maxBreadcrumbs = 100
	}
	return &Scope{
		tags:           make(map[string]string),
		metadata:       make(map[string]any),
		maxBreadcrumbs: maxBreadcrumbs,
	}
}

// Clone returns a deep copy of this Scope, safe for independent mutation.
func (s *Scope) Clone() *Scope {
	s.mu.RLock()
	defer s.mu.RUnlock()

	clone := &Scope{
		maxBreadcrumbs: s.maxBreadcrumbs,
		user:           s.user,
		traceID:        s.traceID,
		spanID:         s.spanID,
		tags:           make(map[string]string, len(s.tags)),
		metadata:       make(map[string]any, len(s.metadata)),
		breadcrumbs:    make([]*Breadcrumb, len(s.breadcrumbs)),
		eventProcessors: make([]EventProcessor, len(s.eventProcessors)),
	}
	for k, v := range s.tags {
		clone.tags[k] = v
	}
	for k, v := range s.metadata {
		clone.metadata[k] = v
	}
	copy(clone.breadcrumbs, s.breadcrumbs)
	copy(clone.eventProcessors, s.eventProcessors)
	return clone
}

// SetTag sets a single key-value tag.
func (s *Scope) SetTag(key, value string) {
	s.mu.Lock()
	s.tags[key] = value
	s.mu.Unlock()
}

// RemoveTag removes a single tag.
func (s *Scope) RemoveTag(key string) {
	s.mu.Lock()
	delete(s.tags, key)
	s.mu.Unlock()
}

// SetUser sets the user context.
func (s *Scope) SetUser(u User) {
	s.mu.Lock()
	s.user = u
	s.mu.Unlock()
}

// SetTraceContext pins a trace and span ID on this scope, overriding automatic
// OTel extraction.
func (s *Scope) SetTraceContext(traceID, spanID string) {
	s.mu.Lock()
	s.traceID = traceID
	s.spanID = spanID
	s.mu.Unlock()
}

// AddBreadcrumb appends a breadcrumb, evicting the oldest entry when at capacity.
// The breadcrumb is deep-copied so the caller may safely reuse or mutate the
// original value after this call returns.
func (s *Scope) AddBreadcrumb(bc *Breadcrumb, beforeFunc func(*Breadcrumb, BreadcrumbHint) *Breadcrumb) {
	if bc == nil {
		return
	}
	// Deep-copy the breadcrumb to isolate it from caller mutations.
	cp := *bc
	if len(bc.Data) > 0 {
		cp.Data = make(map[string]any, len(bc.Data))
		for k, v := range bc.Data {
			cp.Data[k] = v
		}
	}
	if cp.Timestamp.IsZero() {
		cp.Timestamp = time.Now()
	}
	if beforeFunc != nil {
		result := beforeFunc(&cp, nil)
		if result == nil {
			return
		}
		cp = *result
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.breadcrumbs = append(s.breadcrumbs, &cp)
	if len(s.breadcrumbs) > s.maxBreadcrumbs {
		s.breadcrumbs = s.breadcrumbs[len(s.breadcrumbs)-s.maxBreadcrumbs:]
	}
}

// ClearBreadcrumbs removes all breadcrumbs.
func (s *Scope) ClearBreadcrumbs() {
	s.mu.Lock()
	s.breadcrumbs = nil
	s.mu.Unlock()
}

// TraceContext returns the trace and span IDs pinned on this scope.
func (s *Scope) TraceContext() (traceID, spanID string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.traceID, s.spanID
}

// AddEventProcessor appends a per-scope processor.
func (s *Scope) AddEventProcessor(p EventProcessor) {
	s.mu.Lock()
	s.eventProcessors = append(s.eventProcessors, p)
	s.mu.Unlock()
}

// ApplyToEntry merges the scope's state into a copy of entry and returns it.
// Scope tags override entry-level tags. Scope metadata is merged first;
// entry-level metadata wins on collision.
// Returns nil if entry is nil.
func (s *Scope) ApplyToEntry(entry *LogEntry) *LogEntry {
	if entry == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	// shallow copy
	e := *entry

	// tags: scope overrides base (entry tags are the base, scope wins)
	if len(s.tags) > 0 {
		e.Tags = mergeTags(entry.Tags, s.tags)
	}

	// metadata: scope base, entry overrides
	if len(s.metadata) > 0 {
		merged := make(map[string]any, len(s.metadata)+len(entry.Metadata))
		for k, v := range s.metadata {
			merged[k] = v
		}
		for k, v := range entry.Metadata {
			merged[k] = v
		}
		e.Metadata = merged
	}

	// breadcrumbs
	if len(s.breadcrumbs) > 0 {
		crumbs := make([]*Breadcrumb, len(s.breadcrumbs))
		copy(crumbs, s.breadcrumbs)
		e.Breadcrumbs = crumbs
	}

	// user → stored in metadata under "user" key for wire format compatibility
	if s.user != (User{}) {
		if _, exists := e.Metadata["user"]; !exists {
			// e.Metadata may still alias entry.Metadata (no deep-copy happened
			// above when len(s.metadata) == 0). Copy before writing.
			if e.Metadata == nil {
				e.Metadata = make(map[string]any, 1)
			} else if len(s.metadata) == 0 {
				copied := make(map[string]any, len(e.Metadata)+1)
				for k, v := range e.Metadata {
					copied[k] = v
				}
				e.Metadata = copied
			}
			e.Metadata["user"] = s.user
		}
	}

	// trace context from scope (only if not already set by OTel)
	if e.TraceID == "" && s.traceID != "" {
		e.TraceID = s.traceID
	}
	if e.SpanID == "" && s.spanID != "" {
		e.SpanID = s.spanID
	}

	return &e
}

// --- Context helpers ---

// WithScope returns a child context that carries s.
func WithScope(ctx context.Context, s *Scope) context.Context {
	return context.WithValue(ctx, scopeContextKey, s)
}

// ScopeFromContext retrieves the Scope from ctx, returning nil if absent.
func ScopeFromContext(ctx context.Context) *Scope {
	s, _ := ctx.Value(scopeContextKey).(*Scope)
	return s
}

// SetHubOnContext returns a child context carrying hub.
func SetHubOnContext(ctx context.Context, hub *Hub) context.Context {
	return context.WithValue(ctx, hubContextKey, hub)
}

// GetHubFromContext retrieves the Hub from ctx, returning nil if absent.
func GetHubFromContext(ctx context.Context) *Hub {
	h, _ := ctx.Value(hubContextKey).(*Hub)
	return h
}

