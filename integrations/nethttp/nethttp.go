// Package nethttp provides a LogTide middleware for the standard net/http package.
package nethttp

import (
	"net"
	"net/http"
	"strings"
	"time"

	logtide "github.com/logtide-dev/logtide-sdk-go"
)

// Middleware wraps next with per-request LogTide scope isolation.
//
// For each request it:
//  1. Clones the Hub from ctx (or the global Hub if absent).
//  2. Pushes a new Scope and configures it with HTTP request metadata.
//  3. Parses the incoming traceparent header and stores it on the Scope.
//  4. Injects the Hub into the request context.
//  5. After the handler returns, adds a response breadcrumb.
//
// Usage:
//
//	http.Handle("/", nethttp.Middleware(myHandler))
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hub := hubFromRequest(r)
		hub.ConfigureScope(func(s *logtide.Scope) {
			s.SetTag("http.method", r.Method)
			s.SetTag("http.url", r.URL.String())
			s.SetTag("http.host", r.Host)
			if ip := clientIP(r); ip != "" {
				s.SetTag("http.client_ip", ip)
			}

			// Parse and store W3C traceparent if present.
			if tp := r.Header.Get("Traceparent"); tp != "" {
				traceID, spanID, _, err := logtide.ParseTraceparent(tp)
				if err == nil {
					s.SetTraceContext(traceID, spanID)
				}
			}
		})

		hub.AddBreadcrumb(&logtide.Breadcrumb{
			Type:      "http",
			Category:  "request",
			Message:   r.Method + " " + r.URL.Path,
			Level:     logtide.LevelInfo,
			Timestamp: time.Now(),
			Data: map[string]any{
				"method": r.Method,
				"url":    r.URL.String(),
			},
		}, nil)

		ctx := logtide.SetHubOnContext(r.Context(), hub)
		rw := &responseWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rw, r.WithContext(ctx))

		hub.AddBreadcrumb(&logtide.Breadcrumb{
			Type:     "http",
			Category: "response",
			Level:    levelForStatus(rw.status),
			Timestamp: time.Now(),
			Data: map[string]any{
				"status_code": rw.status,
			},
		}, nil)
	})
}

// Integration implements logtide.Integration.
// Register it in ClientOptions.Integrations to have the net/http middleware
// listed in the SDK integration metadata.
type Integration struct{}

func (Integration) Name() string { return "net/http" }

// Setup implements logtide.Integration. The middleware is a standalone function;
// no client-level event processor registration is required.
func (Integration) Setup(_ *logtide.Client) {}

// --- helpers ---

func hubFromRequest(r *http.Request) *logtide.Hub {
	if h := logtide.GetHubFromContext(r.Context()); h != nil {
		return h.Clone()
	}
	return logtide.CurrentHub().Clone()
}

func clientIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		// X-Forwarded-For may be a comma-separated list; take only the first
		// (leftmost) IP, which is the original client address.
		if idx := strings.IndexByte(fwd, ','); idx >= 0 {
			return strings.TrimSpace(fwd[:idx])
		}
		return fwd
	}
	if real := r.Header.Get("X-Real-IP"); real != "" {
		return real
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func levelForStatus(status int) logtide.Level {
	switch {
	case status >= 500:
		return logtide.LevelError
	case status >= 400:
		return logtide.LevelWarn
	default:
		return logtide.LevelInfo
	}
}

// responseWriter wraps http.ResponseWriter to capture the status code.
type responseWriter struct {
	http.ResponseWriter
	status int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

// Flush implements http.Flusher so that streaming handlers (SSE, etc.) work
// correctly through the middleware wrapper.
func (rw *responseWriter) Flush() {
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
