package logtide_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	logtide "github.com/logtide-dev/logtide-sdk-go"
)

// Outbound traceparent injection (conformance C25, spec 005 §2).

func captureHeader(t *testing.T) (*httptest.Server, *string) {
	t.Helper()
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("traceparent")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv, &got
}

func TestWrapTransportInjectsFromScope(t *testing.T) {
	srv, got := captureHeader(t)

	scope := logtide.NewScope(10)
	scope.SetTraceContext("4bf92f3577b34da6a3ce929d0e0e4736", "00f067aa0ba902b7")
	ctx := logtide.WithScope(context.Background(), scope)

	client := &http.Client{Transport: logtide.WrapTransport(nil)}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	resp.Body.Close()

	want := "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	if *got != want {
		t.Fatalf("traceparent = %q, want %q", *got, want)
	}
}

func TestWrapTransportGeneratesSpanIDWhenScopeHasNone(t *testing.T) {
	srv, got := captureHeader(t)

	scope := logtide.NewScope(10)
	scope.SetTraceContext("4bf92f3577b34da6a3ce929d0e0e4736", "")
	ctx := logtide.WithScope(context.Background(), scope)

	client := &http.Client{Transport: logtide.WrapTransport(nil)}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	resp.Body.Close()

	traceID, spanID, sampled, err := logtide.ParseTraceparent(*got)
	if err != nil {
		t.Fatalf("ParseTraceparent(%q): %v", *got, err)
	}
	if traceID != "4bf92f3577b34da6a3ce929d0e0e4736" || !sampled {
		t.Fatalf("traceparent = %q", *got)
	}
	if len(spanID) != 16 || strings.Count(spanID, "0") == 16 {
		t.Fatalf("span id %q not generated", spanID)
	}
}

func TestWrapTransportNoopWithoutTraceContext(t *testing.T) {
	srv, got := captureHeader(t)

	client := &http.Client{Transport: logtide.WrapTransport(nil)}
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	resp.Body.Close()

	if *got != "" {
		t.Fatalf("traceparent should not be set, got %q", *got)
	}
}

func TestWrapTransportDoesNotOverrideExisting(t *testing.T) {
	srv, got := captureHeader(t)

	scope := logtide.NewScope(10)
	scope.SetTraceContext("4bf92f3577b34da6a3ce929d0e0e4736", "00f067aa0ba902b7")
	ctx := logtide.WithScope(context.Background(), scope)

	client := &http.Client{Transport: logtide.WrapTransport(nil)}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	req.Header.Set("traceparent", "00-"+strings.Repeat("a", 32)+"-"+strings.Repeat("b", 16)+"-01")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	resp.Body.Close()

	if !strings.Contains(*got, strings.Repeat("a", 32)) {
		t.Fatalf("existing traceparent overridden: %q", *got)
	}
}
