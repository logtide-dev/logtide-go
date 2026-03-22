package logtide

import (
	"fmt"
	"os"
	"runtime"
)

// Integration augments or filters events within the Client pipeline.
// Integrations are installed once at Client construction time.
// All methods must be safe for concurrent use.
type Integration interface {
	// Name returns a stable unique identifier for this integration.
	Name() string

	// Setup is called once by NewClient after construction.
	// Register processors via client.AddEventProcessor inside Setup.
	Setup(client *Client)
}

// --- Built-in integrations ---

// EnvironmentIntegration attaches runtime context (Go version, OS, architecture)
// to every log entry as metadata.
type EnvironmentIntegration struct{}

func (e *EnvironmentIntegration) Name() string { return "Environment" }

func (e *EnvironmentIntegration) Setup(client *Client) {
	goVersion := runtime.Version()
	goos := runtime.GOOS
	goarch := runtime.GOARCH

	client.AddEventProcessor(func(entry *LogEntry, _ *EventHint) *LogEntry {
		if _, ok := entry.Metadata["runtime"]; ok {
			return entry
		}
		// Copy before mutating to avoid a data race when the caller's metadata
		// map is shared across concurrent log calls.
		meta := make(map[string]any, len(entry.Metadata)+1)
		for k, v := range entry.Metadata {
			meta[k] = v
		}
		meta["runtime"] = map[string]any{
			"go":   goVersion,
			"os":   goos,
			"arch": goarch,
		}
		entry.Metadata = meta
		return entry
	})
}

// GlobalTagsIntegration applies ClientOptions.Tags to every log entry.
type GlobalTagsIntegration struct{}

func (g *GlobalTagsIntegration) Name() string { return "GlobalTags" }

func (g *GlobalTagsIntegration) Setup(client *Client) {
	tags := client.Options().Tags
	if len(tags) == 0 {
		return
	}
	client.AddEventProcessor(func(entry *LogEntry, _ *EventHint) *LogEntry {
		entry.Tags = mergeTags(tags, entry.Tags)
		return entry
	})
}

// defaultIntegrations returns the integration list installed by default.
func defaultIntegrations() []Integration {
	return []Integration{
		&EnvironmentIntegration{},
		&GlobalTagsIntegration{},
	}
}

// setupIntegrations processes the Integrations option, deduplicates by name,
// and calls Setup on each.
func setupIntegrations(client *Client, opts ClientOptions) {
	list := defaultIntegrations()
	if opts.Integrations != nil {
		list = opts.Integrations(list)
	}

	seen := make(map[string]struct{}, len(list))
	for _, i := range list {
		name := i.Name()
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		i.Setup(client)
		client.integrations = append(client.integrations, i)
	}
}

// --- Helpers ---

// mergeTags merges base and override tags into a new map.
// Keys in override win on collision.
func mergeTags(base, override map[string]string) map[string]string {
	merged := make(map[string]string, len(base)+len(override))
	for k, v := range base {
		merged[k] = v
	}
	for k, v := range override {
		merged[k] = v
	}
	return merged
}

// --- Server name helper ---

func resolveServerName(override string) string {
	if override != "" {
		return override
	}
	host, err := os.Hostname()
	if err != nil {
		return ""
	}
	return host
}

// --- Validation ---

func validateEntry(entry *LogEntry) error {
	if entry.Service == "" {
		return ErrServiceRequired
	}
	if len(entry.Service) > 100 {
		return &ValidationError{Field: "service", Message: "service name must be 100 characters or less"}
	}
	if entry.Message == "" {
		return &ValidationError{Field: "message", Message: "message is required"}
	}
	switch entry.Level {
	case LevelDebug, LevelInfo, LevelWarn, LevelError, LevelCritical:
	default:
		return &ValidationError{
			Field:   "level",
			Message: fmt.Sprintf("invalid level %q (must be debug, info, warn, error, or critical)", entry.Level),
		}
	}
	return nil
}
