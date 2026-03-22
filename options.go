package logtide

import (
	"io"
	"net/http"
	"time"
)

// ClientOptions configures a Client.
//
// The recommended way to initialise options is to call NewClientOptions and
// override only the fields you need:
//
//	opts := logtide.NewClientOptions()
//	opts.DSN = "https://lp_abc@api.logtide.dev"
//	opts.Service = "my-service"
//	client, err := logtide.NewClient(opts)
type ClientOptions struct {
	// DSN is the LogTide Data Source Name:
	//   https://{api_key}@{host}
	// Required unless Transport is provided directly.
	DSN string

	// Service is the logical name of the service (required, max 100 chars).
	Service string

	// Release is the application version / release identifier.
	Release string

	// Environment is the deployment environment ("production", "staging", …).
	Environment string

	// ServerName overrides the hostname attached to every log entry.
	// Defaults to os.Hostname().
	ServerName string

	// Debug enables verbose SDK-internal logging to DebugWriter.
	Debug bool

	// DebugWriter is the target for debug output. Defaults to os.Stderr.
	DebugWriter io.Writer

	// MaxBreadcrumbs is the maximum number of breadcrumbs retained per Scope.
	// Default: 100.
	MaxBreadcrumbs int

	// AttachStacktrace causes a stack trace to be attached to every CaptureError call.
	// Default: true. Use Bool(false) to explicitly disable.
	AttachStacktrace *bool

	// SampleRate is a fraction in (0.0, 1.0] controlling the proportion of
	// log entries that are delivered. 1.0 sends everything (default).
	// Values in (0, 1) enable random sampling. To suppress all output in tests
	// or dry-run mode, use NoopTransport instead of setting SampleRate to 0.
	// Default: 1.0.
	SampleRate float64

	// BeforeSend is called before every log entry is dispatched to the Transport.
	// Return nil to drop the entry. The function must be safe for concurrent use.
	BeforeSend func(entry *LogEntry, hint *EventHint) *LogEntry

	// BeforeBreadcrumb is called before a breadcrumb is added to a Scope.
	// Return nil to drop it.
	BeforeBreadcrumb func(bc *Breadcrumb, hint BreadcrumbHint) *Breadcrumb

	// Integrations is applied to the default integration list before installation.
	// Receives the default list; return the list you want active.
	// If nil, all defaults are installed unchanged.
	Integrations func([]Integration) []Integration

	// Transport overrides the default HTTPTransport.
	// Useful for testing (e.g. NoopTransport{}) or custom delivery.
	Transport Transport

	// HTTPClient overrides the net/http.Client used by the default HTTPTransport.
	HTTPClient *http.Client

	// BatchSize is the maximum number of entries per HTTP batch.
	// Default: 100.
	BatchSize int

	// FlushInterval is the maximum time between automatic batch flushes.
	// Default: 5s.
	FlushInterval time.Duration

	// FlushTimeout is used for the final Flush on Close and as the Init
	// return value deadline.
	// Default: 10s.
	FlushTimeout time.Duration

	// MaxRetries for the default HTTPTransport.
	// Default: 3.
	MaxRetries int

	// RetryMinBackoff for the default HTTPTransport.
	// Default: 1s.
	RetryMinBackoff time.Duration

	// RetryMaxBackoff for the default HTTPTransport.
	// Default: 60s.
	RetryMaxBackoff time.Duration

	// CircuitBreakerThreshold is the consecutive-failure count before the
	// circuit opens. Set to 0 to disable.
	// Default: 5.
	CircuitBreakerThreshold int

	// CircuitBreakerTimeout is the recovery probe interval.
	// Default: 30s.
	CircuitBreakerTimeout time.Duration

	// Tags are key-value pairs applied to every log entry.
	Tags map[string]string
}

// Bool returns a pointer to v. Use with pointer-bool fields in ClientOptions
// such as AttachStacktrace to distinguish an explicit false from the zero value.
func Bool(v bool) *bool { return &v }

// NewClientOptions returns ClientOptions pre-filled with safe defaults.
func NewClientOptions() ClientOptions {
	return ClientOptions{
		MaxBreadcrumbs:          100,
		AttachStacktrace:        Bool(true),
		SampleRate:              1.0,
		BatchSize:               100,
		FlushInterval:           5 * time.Second,
		FlushTimeout:            10 * time.Second,
		MaxRetries:              3,
		RetryMinBackoff:         1 * time.Second,
		RetryMaxBackoff:         60 * time.Second,
		CircuitBreakerThreshold: 5,
		CircuitBreakerTimeout:   30 * time.Second,
	}
}
