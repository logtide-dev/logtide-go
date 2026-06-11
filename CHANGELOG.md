# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Fixed

- **Wire format now matches the ingest schema.** Log entries were serialised with `timestamp` and top-level `event_id`, `tags`, `breadcrumbs`, `errors`, `release`, `environment` and `server_name` fields; the backend only accepts `time`, `service`, `level`, `message`, `metadata`, `trace_id`, `span_id` and `session_id`, and silently dropped everything else — including the original timestamp (replaced by ingestion time), breadcrumbs and captured exceptions. `LogEntry` now marshals to the correct contract: `time` (RFC 3339, UTC) plus all SDK fields nested inside `metadata`, with exceptions converted to the backend's structured `metadata.exception` format (`type`, `message`, `language: "go"`, `stacktrace`, nested `cause` chain) so server-side error grouping and fingerprinting work
- `LogEntry` also gained a symmetric `UnmarshalJSON` so wire-format payloads round-trip back into the struct

## [0.8.4] - 2026-03-22

### Fixed

- `Scope.ApplyToEntry` no longer mutates the caller's `Metadata` map when the scope has a user but no scope-level metadata
- `otelexport`: completed span trace IDs are now correctly preserved; an ambient active span in the exporter context can no longer override them
- `retry.Do` now drains the response body and returns an error when retries are exhausted on a retryable HTTP status (was returning `nil` error with an open body)
- `tryExtractPkgErrorsStack` no longer panics when an error interface holds a nil concrete pointer
- Client-level event processor slice is now safely copied under lock before iteration, preventing a data race with concurrent `AddEventProcessor` calls
- `Client.Options()` Tags map is now copied inside the read lock, preventing a race if the caller mutates the original map concurrently
- `EnvironmentIntegration` processor copies the metadata map before adding the `runtime` key instead of mutating the entry's existing map
- `Hub.Init` flush timeout now correctly uses `client.Options().FlushTimeout` instead of a hardcoded value

### Changed

- `Client` log methods (`Debug`, `Info`, `Warn`, `Error`, `Critical`) now return `EventID` instead of `error`, consistent with the `Hub` API
- `ClientOptions.AttachStacktrace` changed from `bool` to `*bool`; use the new `Bool(v)` helper to set it explicitly and distinguish `false` from the zero value
- `retry.Do` returns an error (instead of `(resp, nil)`) when the maximum retry count is exhausted on a retryable HTTP status

### Removed

- `Scope.SetTags` and `Scope.SetMetadata` removed (use `SetTag` and pass metadata directly to log methods)
- `HasHubOnContext` removed (use `GetHubFromContext(ctx) != nil` directly)

## [0.1.0] - 2026-01-13

### Added

- Initial release of LogTide Go SDK
- Leveled logging: Debug, Info, Warn, Error, Critical
- Automatic batching with configurable size and interval
- Retry logic with exponential backoff and jitter
- Circuit breaker pattern for fault tolerance
- Graceful shutdown with context support
- OpenTelemetry integration for automatic trace ID extraction
- Thread-safe operations
- ~87% test coverage
- Framework integration examples:
  - Gin middleware
  - Echo middleware
  - Chi middleware
  - Standard library net/http middleware
- Complete documentation:
  - Installation guide
  - Quick start guide
  - Framework integrations guide

[0.8.4]: https://github.com/logtide-dev/logtide-sdk-go/releases/tag/v0.8.4
[0.1.0]: https://github.com/logtide-dev/logtide-sdk-go/releases/tag/v0.1.0
