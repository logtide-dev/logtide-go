package nethttp_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	logtide "github.com/logtide-dev/logtide-sdk-go"
	lnethttp "github.com/logtide-dev/logtide-sdk-go/integrations/nethttp"
)

func newTestClient(t *testing.T) *logtide.Client {
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

func TestMiddlewareInjectsHubIntoContext(t *testing.T) {
	client := newTestClient(t)
	hub := logtide.NewHub(client, nil)

	var gotHub *logtide.Hub
	handler := lnethttp.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHub = logtide.GetHubFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	ctx := logtide.SetHubOnContext(req.Context(), hub)
	req = req.WithContext(ctx)
	rw := httptest.NewRecorder()
	handler.ServeHTTP(rw, req)

	if gotHub == nil {
		t.Error("hub should be injected into request context by middleware")
	}
}

func TestMiddlewareSetsHTTPTags(t *testing.T) {
	client := newTestClient(t)
	hub := logtide.NewHub(client, nil)

	var scope *logtide.Scope
	handler := lnethttp.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h := logtide.GetHubFromContext(r.Context()); h != nil {
			scope = h.Scope()
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/things?x=1", nil)
	req.Host = "example.com"
	ctx := logtide.SetHubOnContext(req.Context(), hub)
	req = req.WithContext(ctx)
	rw := httptest.NewRecorder()
	handler.ServeHTTP(rw, req)

	if scope == nil {
		t.Fatal("expected scope from hub in context")
	}

	entry := &logtide.LogEntry{Service: "svc", Level: logtide.LevelInfo, Message: "msg"}
	enriched := scope.ApplyToEntry(entry)

	if enriched.Tags["http.method"] != "POST" {
		t.Errorf("http.method = %q, want POST", enriched.Tags["http.method"])
	}
	if enriched.Tags["http.host"] != "example.com" {
		t.Errorf("http.host = %q, want example.com", enriched.Tags["http.host"])
	}
}

func TestMiddlewareParsesTraceparent(t *testing.T) {
	client := newTestClient(t)
	hub := logtide.NewHub(client, nil)

	var scope *logtide.Scope
	handler := lnethttp.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h := logtide.GetHubFromContext(r.Context()); h != nil {
			scope = h.Scope()
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	ctx := logtide.SetHubOnContext(req.Context(), hub)
	req = req.WithContext(ctx)
	rw := httptest.NewRecorder()
	handler.ServeHTTP(rw, req)

	if scope == nil {
		t.Fatal("expected scope in context")
	}
	traceID, spanID := scope.TraceContext()
	if traceID != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Errorf("traceID = %q, want 4bf92f3577b34da6a3ce929d0e0e4736", traceID)
	}
	if spanID != "00f067aa0ba902b7" {
		t.Errorf("spanID = %q, want 00f067aa0ba902b7", spanID)
	}
}

func TestMiddlewareAddsResponseBreadcrumb(t *testing.T) {
	client := newTestClient(t)
	hub := logtide.NewHub(client, nil)

	var afterScope *logtide.Scope
	var innerHub *logtide.Hub
	handler := lnethttp.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		innerHub = logtide.GetHubFromContext(r.Context())
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	ctx := logtide.SetHubOnContext(req.Context(), hub)
	req = req.WithContext(ctx)
	rw := httptest.NewRecorder()
	handler.ServeHTTP(rw, req)

	if innerHub == nil {
		t.Fatal("innerHub should not be nil")
	}
	afterScope = innerHub.Scope()

	entry := &logtide.LogEntry{Service: "svc", Level: logtide.LevelInfo, Message: "msg"}
	enriched := afterScope.ApplyToEntry(entry)
	// Should have at least the request breadcrumb + the response breadcrumb.
	if len(enriched.Breadcrumbs) < 2 {
		t.Errorf("breadcrumbs = %d, want >= 2 (request + response)", len(enriched.Breadcrumbs))
	}
}

func TestResponseWriterCapturesStatusCode(t *testing.T) {
	// Verify that the middleware's responseWriter wrapper correctly captures the
	// status code and passes it to the response breadcrumb added after the handler.
	client := newTestClient(t)
	hub := logtide.NewHub(client, nil)

	var innerHub *logtide.Hub
	handler := lnethttp.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		innerHub = logtide.GetHubFromContext(r.Context())
		w.WriteHeader(http.StatusTeapot)
	}))

	req := httptest.NewRequest(http.MethodGet, "/teapot", nil)
	ctx := logtide.SetHubOnContext(req.Context(), hub)
	req = req.WithContext(ctx)
	rw := httptest.NewRecorder()
	handler.ServeHTTP(rw, req)

	if rw.Code != http.StatusTeapot {
		t.Errorf("response recorder code = %d, want %d", rw.Code, http.StatusTeapot)
	}
	if innerHub == nil {
		t.Fatal("innerHub should not be nil")
	}

	// Verify the response breadcrumb captured the correct status from the wrapper.
	scope := innerHub.Scope()
	entry := scope.ApplyToEntry(&logtide.LogEntry{Service: "svc", Level: logtide.LevelInfo, Message: "m"})
	if entry == nil {
		t.Fatal("ApplyToEntry returned nil")
	}
	var responseBreadcrumb *logtide.Breadcrumb
	for _, bc := range entry.Breadcrumbs {
		if bc.Category == "response" {
			responseBreadcrumb = bc
			break
		}
	}
	if responseBreadcrumb == nil {
		t.Fatal("no response breadcrumb found")
	}
	if got, ok := responseBreadcrumb.Data["status_code"]; !ok || got != http.StatusTeapot {
		t.Errorf("response breadcrumb status_code = %v, want %d", got, http.StatusTeapot)
	}
}

func TestMiddlewareDefaultStatus200(t *testing.T) {
	// When the handler doesn't call WriteHeader, status defaults to 200.
	handler := lnethttp.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rw := httptest.NewRecorder()
	handler.ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Errorf("default status = %d, want 200", rw.Code)
	}
}

func TestIntegrationName(t *testing.T) {
	i := lnethttp.Integration{}
	if i.Name() != "net/http" {
		t.Errorf("Name() = %q, want net/http", i.Name())
	}
}
