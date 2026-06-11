package logtide

import "net/http"

// traceparentTransport injects the W3C traceparent header on outbound
// requests (spec 005 §2, conformance C25).
type traceparentTransport struct {
	base http.RoundTripper
}

// WrapTransport returns an http.RoundTripper that injects the W3C
// traceparent header on outbound requests, propagating the current trace
// context to downstream services:
//
//	client := &http.Client{Transport: logtide.WrapTransport(nil)}
//
// Source order: active OTel span in the request context, then the Scope
// carried by the context (a scope without a span ID gets a fresh one).
// Requests with no active trace context, or with a traceparent header
// already set, pass through untouched. A nil base uses
// http.DefaultTransport.
func WrapTransport(base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return &traceparentTransport{base: base}
}

// RoundTrip implements http.RoundTripper.
func (t *traceparentTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Header.Get("traceparent") != "" {
		return t.base.RoundTrip(req)
	}

	ctx := req.Context()
	traceID, spanID := traceContextFromContext(ctx) // active OTel span
	if traceID == "" {
		if scope := scopeFromContextOrHub(ctx); scope != nil {
			traceID, spanID = scope.TraceContext()
		}
	}
	if traceID == "" {
		return t.base.RoundTrip(req)
	}
	if spanID == "" {
		// traceparent requires a parent id; mint one for this hop
		spanID = string(newEventID())[:16]
	}

	// Per http.RoundTripper contract the request must not be mutated.
	clone := req.Clone(ctx)
	clone.Header.Set("traceparent", FormatTraceparent(traceID, spanID, true))
	return t.base.RoundTrip(clone)
}
