# Conformance

Scenario-by-scenario status of this SDK against the LogTide SDK contract.
Each scenario ID is stable across all official SDKs; "n/a" entries explain
why a scenario does not apply. TODO entries are tracked work.

| ID | Scenario | Status | Test reference |
|---|---|---|---|
| C01 | basic log: one POST to /api/v1/ingest with X-API-Key, {logs:[...]} body, RFC 3339 time, metadata.sdk | ✅ | `types_test.go` (wire format, sdk stamp), `client_test.go` |
| C02 | batch by size: batchSize entries flush automatically, order preserved | ✅ | `internal/batch/batch_test.go` (size-triggered flush) |
| C03 | batch by interval: entries delivered without explicit flush | ✅ | `internal/batch/batch_test.go` (interval flush) |
| C04 | wire format strictness: SDK fields nested in metadata, only contract fields top-level | ✅ | `types_test.go:TestLogEntryMarshalWireFormat` |
| C05 | exception capture: structured metadata.exception with type/message/language/frames/cause | ✅ | `types_test.go` (metadata.exception, cause chain) |
| C06 | exception chain cap: cause depth ≤ 10, no infinite loop on cycles | TODO | add depth-cap test (marshal caps via backend; SDK does not yet cap) |
| C07 | retry on 5xx with growing backoff | ✅ | `internal/retry/retry_test.go` |
| C08 | no retry on permanent 4xx (400/401/403/413) | ✅ | `internal/retry/retry_test.go` (only 408/429/5xx retried) |
| C09 | Retry-After overrides computed backoff | ✅ | `internal/retry/retry_test.go:TestRetryAfterOverridesBackoff` |
| C10 | circuit breaker opens after threshold failures | ✅ | `internal/circuitbreaker/circuitbreaker_test.go` |
| C11 | circuit breaker half-open probe and recovery | ✅ | `internal/circuitbreaker/circuitbreaker_test.go` (half-open probe) |
| C12 | buffer cap: drops beyond maxBufferSize, counted, never throws | TODO | batch engine bounds memory; add explicit drop-count test |
| C13 | flush on close; capture after close is a silent no-op | ✅ | `client_test.go` (Close flush, post-close drop) |
| C14 | DSN parsing incl. base path; invalid DSN fails at init | ✅ | `dsn_test.go` (incl. base path) |
| C15 | inbound traceparent lands on entry trace_id | ✅ | `integrations/nethttp/nethttp_test.go` (traceparent) |
| C16 | no PII by default; API key never logged | ✅ | explicit-only user context; key only in header (`httpclient`) |
| C17 | serialisation robustness: circular/unserialisable values never throw | partial | encoding/json semantics; circular structs are a caller error in Go |
| C18 | timestamp fidelity: time reflects capture, not delivery | ✅ | `types_test.go` (time from entry timestamp) |
| C20 | scope isolation across concurrent requests | ✅ | `scope_test.go`, `hub_test.go` (Clone isolation) |
| C21 | breadcrumb ring buffer eviction, oldest first | ✅ | `scope_test.go` (breadcrumb eviction) |
| C22 | beforeSend can mutate or drop entries | ✅ | `client_test.go` (BeforeSend) |
| C23 | sampling: rate 0 sends nothing (logs) / no-op spans (traces) | ✅ | `client_test.go` (SampleRate) |
| C24 | OTLP span export with service.name resource | ✅ | `integrations/otelexport/otelexport_test.go` (OTel-native path) |
| C25 | outbound traceparent injection on instrumented HTTP clients | ✅ | `WrapTransport` (`roundtripper_test.go`) |
| C26 | log/trace correlation: active span ids on entries | ✅ | `tracing_test.go` (OTel context extraction) |
| C27 | middleware error capture rethrows after logging | ✅ | `integrations/nethttp/nethttp_test.go` |
| C28 | logging-bridge level mapping and scope context | ✅ | `integrations/logtideslog/logtideslog_test.go` |
