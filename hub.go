package logtide

import (
	"context"
	"sync"
	"time"
)

// layer is one entry in the Hub's scope stack.
type layer struct {
	client *Client
	scope  *Scope
}

// Hub pairs a Client with a stack of Scopes.
//
// The global Hub singleton is accessed via CurrentHub(). Per-request isolation
// is achieved by cloning the Hub with Clone() at request boundaries:
//
//	func middleware(next http.Handler) http.Handler {
//	    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
//	        hub := logtide.CurrentHub().Clone()
//	        hub.ConfigureScope(func(s *logtide.Scope) {
//	            s.SetTag("request_id", r.Header.Get("X-Request-ID"))
//	        })
//	        ctx := logtide.SetHubOnContext(r.Context(), hub)
//	        next.ServeHTTP(w, r.WithContext(ctx))
//	    })
//	}
type Hub struct {
	mu          sync.RWMutex
	stack       []*layer
	lastEventID EventID
}

// NewHub creates a Hub with the given client and initial scope.
// If scope is nil, an empty Scope is created using client.Options().MaxBreadcrumbs.
func NewHub(client *Client, scope *Scope) *Hub {
	if scope == nil {
		if client != nil {
			scope = NewScope(client.opts.MaxBreadcrumbs)
		} else {
			scope = NewScope(100)
		}
	}
	return &Hub{
		stack: []*layer{{client: client, scope: scope}},
	}
}

// Client returns the Client at the top of the stack.
func (h *Hub) Client() *Client {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.top().client
}

// Scope returns the Scope at the top of the stack.
func (h *Hub) Scope() *Scope {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.top().scope
}

// PushScope clones the current top Scope and pushes a new layer reusing
// the same Client. Returns the new child Scope for configuration.
// Callers must pair with PopScope.
func (h *Hub) PushScope() *Scope {
	h.mu.Lock()
	defer h.mu.Unlock()

	top := h.top()
	child := top.scope.Clone()
	h.stack = append(h.stack, &layer{client: top.client, scope: child})
	return child
}

// PopScope removes the top layer. It is a no-op if the stack has only one layer.
func (h *Hub) PopScope() {
	h.mu.Lock()
	defer h.mu.Unlock()

	if len(h.stack) > 1 {
		h.stack = h.stack[:len(h.stack)-1]
	}
}

// WithScope executes fn in a temporary child scope, then restores the previous one.
// It is equivalent to PushScope / defer PopScope.
func (h *Hub) WithScope(fn func(scope *Scope)) {
	scope := h.PushScope()
	defer h.PopScope()
	fn(scope)
}

// ConfigureScope calls fn with the current top Scope for in-place mutation.
func (h *Hub) ConfigureScope(fn func(scope *Scope)) {
	h.mu.RLock()
	scope := h.top().scope
	h.mu.RUnlock()
	fn(scope)
}

// BindClient replaces the Client on the current top layer.
func (h *Hub) BindClient(client *Client) {
	h.mu.Lock()
	h.top().client = client
	h.mu.Unlock()
}

// Clone returns a new Hub with a deep copy of the current top layer.
// Use this at request boundaries to get per-request scope isolation.
func (h *Hub) Clone() *Hub {
	h.mu.RLock()
	top := h.top()
	client := top.client
	scope := top.scope.Clone()
	h.mu.RUnlock()

	return &Hub{
		stack: []*layer{{client: client, scope: scope}},
	}
}

// AddBreadcrumb adds a breadcrumb to the current top Scope.
func (h *Hub) AddBreadcrumb(bc *Breadcrumb, hint BreadcrumbHint) {
	h.mu.RLock()
	scope := h.top().scope
	var beforeFn func(*Breadcrumb, BreadcrumbHint) *Breadcrumb
	if c := h.top().client; c != nil {
		beforeFn = c.opts.BeforeBreadcrumb
	}
	h.mu.RUnlock()

	scope.AddBreadcrumb(bc, beforeFn)
}

// LastEventID returns the EventID of the most recently captured entry.
func (h *Hub) LastEventID() EventID {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.lastEventID
}

// --- Log-level methods ---

// Debug captures a debug-level log entry via the Hub's Client and Scope.
func (h *Hub) Debug(ctx context.Context, message string, metadata map[string]any) EventID {
	return h.capture(ctx, LevelDebug, message, metadata)
}

// Info captures an info-level log entry.
func (h *Hub) Info(ctx context.Context, message string, metadata map[string]any) EventID {
	return h.capture(ctx, LevelInfo, message, metadata)
}

// Warn captures a warn-level log entry.
func (h *Hub) Warn(ctx context.Context, message string, metadata map[string]any) EventID {
	return h.capture(ctx, LevelWarn, message, metadata)
}

// Error captures an error-level log entry.
func (h *Hub) Error(ctx context.Context, message string, metadata map[string]any) EventID {
	return h.capture(ctx, LevelError, message, metadata)
}

// Critical captures a critical-level log entry.
func (h *Hub) Critical(ctx context.Context, message string, metadata map[string]any) EventID {
	return h.capture(ctx, LevelCritical, message, metadata)
}

// CaptureError captures err as an error-level entry with a stack trace.
// Returns the EventID of the entry, or "" if it was dropped.
func (h *Hub) CaptureError(ctx context.Context, err error, metadata map[string]any) EventID {
	client := h.Client()
	if client == nil {
		return ""
	}
	ctx = h.injectScopeIfMissing(ctx)
	id := client.CaptureError(ctx, err, metadata)
	h.recordEventID(id)
	return id
}

// Flush flushes all buffered entries using timeout as the deadline.
// Returns true if all entries were delivered before the deadline.
func (h *Hub) Flush(timeout time.Duration) bool {
	client := h.Client()
	if client == nil {
		return true
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return client.Flush(ctx)
}

// --- internal ---

func (h *Hub) top() *layer {
	return h.stack[len(h.stack)-1]
}

func (h *Hub) capture(ctx context.Context, level Level, message string, metadata map[string]any) EventID {
	client := h.Client()
	if client == nil {
		return ""
	}
	ctx = h.injectScopeIfMissing(ctx)

	entry := &LogEntry{Level: level, Message: message}
	if metadata != nil {
		entry.Metadata = metadata
	}
	id := client.captureEntry(ctx, entry, nil)
	h.recordEventID(id)
	return id
}

// injectScopeIfMissing ensures the hub's scope is available in ctx.
func (h *Hub) injectScopeIfMissing(ctx context.Context) context.Context {
	if ScopeFromContext(ctx) != nil {
		return ctx
	}
	return WithScope(ctx, h.Scope())
}

func (h *Hub) recordEventID(id EventID) {
	if id == "" {
		return
	}
	h.mu.Lock()
	h.lastEventID = id
	h.mu.Unlock()
}

// --- Global singleton ---

var (
	globalHub   *Hub
	globalHubMu sync.RWMutex
)

func init() {
	// Start with a no-op hub (nil client) so package-level calls before Init
	// are silently dropped rather than panicking.
	globalHub = NewHub(nil, NewScope(100))
}

// CurrentHub returns the global Hub singleton.
func CurrentHub() *Hub {
	globalHubMu.RLock()
	defer globalHubMu.RUnlock()
	return globalHub
}

// Init initialises the global Hub with a new Client built from opts.
//
// It returns a flush function that should be deferred at program startup to
// ensure all buffered entries are delivered on shutdown:
//
//	flush := logtide.Init(logtide.ClientOptions{
//	    DSN:     "https://lp_abc@api.logtide.dev",
//	    Service: "my-service",
//	})
//	defer flush()
//
// Init may be called multiple times; each call replaces the global Client.
func Init(opts ClientOptions) func() {
	client, err := NewClient(opts)
	if err != nil {
		if opts.Debug && opts.DebugWriter != nil {
			logDebug(opts.DebugWriter, "Init failed: %v", err)
		}
		return func() {}
	}

	hub := NewHub(client, NewScope(opts.MaxBreadcrumbs))

	globalHubMu.Lock()
	globalHub = hub
	globalHubMu.Unlock()

	return func() { hub.Flush(client.Options().FlushTimeout) }
}

// --- Package-level API (delegates to hub from ctx or global hub) ---

func hubFrom(ctx context.Context) *Hub {
	if h := GetHubFromContext(ctx); h != nil {
		return h
	}
	return CurrentHub()
}

// Debug captures a debug-level log entry via the current Hub.
func Debug(ctx context.Context, message string, metadata map[string]any) EventID {
	return hubFrom(ctx).Debug(ctx, message, metadata)
}

// Info captures an info-level log entry via the current Hub.
func Info(ctx context.Context, message string, metadata map[string]any) EventID {
	return hubFrom(ctx).Info(ctx, message, metadata)
}

// Warn captures a warn-level log entry via the current Hub.
func Warn(ctx context.Context, message string, metadata map[string]any) EventID {
	return hubFrom(ctx).Warn(ctx, message, metadata)
}

// Error captures an error-level log entry via the current Hub.
func Error(ctx context.Context, message string, metadata map[string]any) EventID {
	return hubFrom(ctx).Error(ctx, message, metadata)
}

// Critical captures a critical-level log entry via the current Hub.
func Critical(ctx context.Context, message string, metadata map[string]any) EventID {
	return hubFrom(ctx).Critical(ctx, message, metadata)
}

// CaptureError captures err as an error-level entry with a stack trace.
func CaptureError(ctx context.Context, err error, metadata map[string]any) EventID {
	return hubFrom(ctx).CaptureError(ctx, err, metadata)
}

// AddBreadcrumb adds a breadcrumb to the current Hub's top Scope.
func AddBreadcrumb(ctx context.Context, bc *Breadcrumb, hint BreadcrumbHint) {
	hubFrom(ctx).AddBreadcrumb(bc, hint)
}

// Flush flushes all buffered entries using the global Hub's flush timeout.
// Returns true if all entries were delivered.
func Flush(timeout time.Duration) bool {
	return CurrentHub().Flush(timeout)
}
