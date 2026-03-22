<p align="center">
  <img src="https://raw.githubusercontent.com/logtide-dev/logtide/main/docs/images/logo.png" alt="LogTide Logo" width="400">
</p>

<h1 align="center">LogTide Go SDK</h1>

<p align="center">
  <a href="https://pkg.go.dev/github.com/logtide-dev/logtide-sdk-go"><img src="https://pkg.go.dev/badge/github.com/logtide-dev/logtide-sdk-go.svg" alt="Go Reference"></a>
  <a href="https://goreportcard.com/report/github.com/logtide-dev/logtide-sdk-go"><img src="https://goreportcard.com/badge/github.com/logtide-dev/logtide-sdk-go" alt="Go Report Card"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/License-MIT-blue.svg" alt="License"></a>
  <a href="https://github.com/logtide-dev/logtide-sdk-go/releases"><img src="https://img.shields.io/github/v/release/logtide-dev/logtide-sdk-go" alt="Release"></a>
</p>

<p align="center">
  Official Go SDK for <a href="https://logtide.dev">LogTide</a> — structured logging with automatic batching, retry, circuit breaker, and OpenTelemetry integration.
</p>

---

## Features

- **Leveled logging** — Debug, Info, Warn, Error, Critical, CaptureError
- **Hub / Scope model** — per-request context isolation with breadcrumbs, tags, and user metadata
- **Automatic batching** — configurable batch size and flush interval
- **Retry with backoff** — exponential backoff with jitter
- **Circuit breaker** — prevents cascading failures
- **OpenTelemetry** — trace/span IDs extracted automatically; span exporter included
- **net/http middleware** — per-request scope isolation out of the box
- **Thread-safe** — safe for concurrent use

## Requirements

- Go 1.23 or later
- A LogTide account and DSN

## Installation

```bash
go get github.com/logtide-dev/logtide-sdk-go
```

---

## Quick Start

### Global singleton (recommended for most apps)

```go
package main

import (
    "context"
    logtide "github.com/logtide-dev/logtide-sdk-go"
)

func main() {
    flush := logtide.Init(logtide.ClientOptions{
        DSN:         "https://lp_your_api_key@api.logtide.dev",
        Service:     "my-service",
        Environment: "production",
        Release:     "v1.2.3",
    })
    defer flush()

    logtide.Info(context.Background(), "Hello LogTide!", nil)
    logtide.Error(context.Background(), "Something went wrong", map[string]any{
        "user_id": 42,
    })
}
```

### Explicit client

```go
opts := logtide.NewClientOptions()
opts.DSN     = "https://lp_your_api_key@api.logtide.dev"
opts.Service = "my-service"

client, err := logtide.NewClient(opts)
if err != nil {
    log.Fatal(err)
}
defer client.Close()

id := client.Info(context.Background(), "Hello", nil)
fmt.Println("event id:", id)
```

---

## DSN format

```
https://{api_key}@{host}
```

Example: `https://lp_abc123@api.logtide.dev`

---

## Configuration

```go
opts := logtide.NewClientOptions()
opts.DSN                    = "https://lp_abc@api.logtide.dev"
opts.Service                = "my-service"       // required
opts.Release                = "v1.2.3"
opts.Environment            = "production"
opts.Tags                   = map[string]string{"region": "eu-west-1"}
opts.BatchSize              = 100                // entries per HTTP batch
opts.FlushInterval          = 5 * time.Second
opts.FlushTimeout           = 10 * time.Second
opts.MaxRetries             = 3
opts.RetryMinBackoff        = 1 * time.Second
opts.RetryMaxBackoff        = 60 * time.Second
opts.CircuitBreakerThreshold = 5               // consecutive failures before open
opts.CircuitBreakerTimeout  = 30 * time.Second
opts.AttachStacktrace       = logtide.Bool(true)
```

---

## Logging

All log methods return the `EventID` assigned to the entry, or `""` if the entry was dropped.

```go
ctx := context.Background()

client.Debug(ctx, "cache miss", map[string]any{"key": "user:42"})
client.Info(ctx, "request handled", map[string]any{"status": 200, "ms": 12})
client.Warn(ctx, "rate limit approaching", nil)
client.Error(ctx, "db query failed", map[string]any{"query": "SELECT ..."})
client.Critical(ctx, "out of memory", nil)

// Capture an error with full stack trace
if err := doSomething(); err != nil {
    client.CaptureError(ctx, err, map[string]any{"op": "doSomething"})
}
```

---

## Hub & Scope

The Hub/Scope model lets you attach contextual data (tags, breadcrumbs, user info, trace context) to all log entries within a logical unit of work.

```go
// Configure the global scope
logtide.ConfigureScope(func(s *logtide.Scope) {
    s.SetTag("region", "eu-west-1")
    s.SetUser(logtide.User{ID: "u123", Email: "alice@example.com"})
})

// Per-request isolation via PushScope / PopScope
logtide.PushScope()
defer logtide.PopScope()

logtide.ConfigureScope(func(s *logtide.Scope) {
    s.SetTag("request_id", requestID)
    s.AddBreadcrumb(&logtide.Breadcrumb{
        Category: "auth",
        Message:  "user authenticated",
        Level:    logtide.LevelInfo,
        Timestamp: time.Now(),
    }, nil)
})

logtide.Info(ctx, "processing order", nil) // includes request_id tag + breadcrumb
```

---

## net/http middleware

```go
import lnethttp "github.com/logtide-dev/logtide-sdk-go/integrations/nethttp"

http.Handle("/", lnethttp.Middleware(myHandler))
```

The middleware automatically:
- clones the Hub for each request (scope isolation)
- sets `http.method`, `http.url`, `http.host`, `http.client_ip` tags
- parses the `Traceparent` header and stores trace/span IDs on the scope
- adds request and response breadcrumbs

---

## OpenTelemetry

### Automatic trace context extraction

Trace and span IDs are extracted automatically from any active OTel span in the context:

```go
ctx, span := tracer.Start(ctx, "process-order")
defer span.End()

// trace_id and span_id are included automatically
client.Info(ctx, "order processed", map[string]any{"order_id": 99})
```

### Span exporter

Export completed spans to LogTide:

```go
import "github.com/logtide-dev/logtide-sdk-go/integrations/otelexport"

integration := otelexport.New()

flush := logtide.Init(logtide.ClientOptions{
    DSN:     "https://lp_abc@api.logtide.dev",
    Service: "my-service",
    Integrations: func(defaults []logtide.Integration) []logtide.Integration {
        return append(defaults, integration)
    },
})
defer flush()

tp := sdktrace.NewTracerProvider(
    sdktrace.WithBatcher(integration.Exporter()),
)
```

---

## Flush & shutdown

```go
// Flush with deadline
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()
client.Flush(ctx)

// Close flushes and releases all resources
client.Close()
```

---

## BeforeSend hook

Inspect or drop entries before they are sent:

```go
opts.BeforeSend = func(entry *logtide.LogEntry, hint *logtide.EventHint) *logtide.LogEntry {
    // drop health-check noise
    if entry.Message == "health check" {
        return nil
    }
    return entry
}
```

---

## Testing

Use `NoopTransport` to silence all output in tests:

```go
client, _ := logtide.NewClient(logtide.ClientOptions{
    Service:   "test",
    Transport: logtide.NoopTransport{},
})
```

---

## Examples

| Example | Description |
|---------|-------------|
| [examples/basic](./examples/basic) | All log levels, metadata, CaptureError |
| [examples/gin](./examples/gin) | Gin framework integration |
| [examples/echo](./examples/echo) | Echo framework integration |
| [examples/stdlib](./examples/stdlib) | Standard library net/http |
| [examples/otel](./examples/otel) | OpenTelemetry distributed tracing |

---

## API reference

- **Online:** [pkg.go.dev/github.com/logtide-dev/logtide-sdk-go](https://pkg.go.dev/github.com/logtide-dev/logtide-sdk-go)
- **Local:** `godoc -http=:6060`

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

MIT — see [LICENSE](LICENSE).
