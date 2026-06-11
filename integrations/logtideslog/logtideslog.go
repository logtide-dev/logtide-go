// Package logtideslog provides a log/slog Handler that routes records
// through a LogTide client, so existing slog-based logging flows to LogTide
// without code changes:
//
//	client, _ := logtide.NewClient(logtide.ClientOptions{DSN: dsn, Service: "api"})
//	logger := slog.New(logtideslog.New(client, nil))
//	slog.SetDefault(logger)
//
// Records honour the client's full pipeline (scope merge, processors,
// BeforeSend, sampling, batching). Attributes become entry metadata, slog
// groups become nested metadata objects, and attribute values implementing
// error are promoted to structured exceptions so server-side error grouping
// works.
package logtideslog

import (
	"context"
	"log/slog"

	logtide "github.com/logtide-dev/logtide-sdk-go"
)

// Options configures the Handler.
type Options struct {
	// Level is the minimum record level the handler accepts.
	// Defaults to slog.LevelInfo.
	Level slog.Leveler
}

// Handler implements slog.Handler on top of a LogTide client.
type Handler struct {
	client *logtide.Client
	level  slog.Leveler
	attrs  []slog.Attr
	groups []string
}

var _ slog.Handler = (*Handler)(nil)

// New creates a Handler routing records through client. opts may be nil.
func New(client *logtide.Client, opts *Options) *Handler {
	level := slog.Leveler(slog.LevelInfo)
	if opts != nil && opts.Level != nil {
		level = opts.Level
	}
	return &Handler{client: client, level: level}
}

// Enabled implements slog.Handler.
func (h *Handler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level.Level()
}

// Handle implements slog.Handler.
func (h *Handler) Handle(ctx context.Context, record slog.Record) error {
	metadata := make(map[string]any, len(h.attrs)+record.NumAttrs())
	var errs []logtide.Exception

	// Pre-bound attrs were group-qualified at bind time (WithAttrs), so they
	// resolve from the top level; record attrs nest under the open groups.
	for _, attr := range h.attrs {
		addAttr(metadata, attr, &errs)
	}
	recordTarget := groupTarget(metadata, h.groups)
	record.Attrs(func(attr slog.Attr) bool {
		addAttr(recordTarget, attr, &errs)
		return true
	})

	entry := &logtide.LogEntry{
		Level:   mapLevel(record.Level),
		Message: record.Message,
		Errors:  errs,
	}
	if !record.Time.IsZero() {
		entry.Timestamp = record.Time
	}
	if len(metadata) > 0 {
		entry.Metadata = metadata
	}

	h.client.CaptureEntry(ctx, entry)
	return nil
}

// WithAttrs implements slog.Handler.
func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	clone := *h
	// qualify with the open groups so nesting is preserved
	qualified := make([]slog.Attr, 0, len(attrs))
	for _, attr := range attrs {
		qualified = append(qualified, nestAttr(attr, h.groups))
	}
	clone.attrs = append(append([]slog.Attr{}, h.attrs...), qualified...)
	return &clone
}

// WithGroup implements slog.Handler.
func (h *Handler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	clone := *h
	clone.groups = append(append([]string{}, h.groups...), name)
	return &clone
}

// mapLevel converts slog levels to LogTide levels. Levels at or above
// slog.LevelError+4 map to critical.
func mapLevel(level slog.Level) logtide.Level {
	switch {
	case level >= slog.LevelError+4:
		return logtide.LevelCritical
	case level >= slog.LevelError:
		return logtide.LevelError
	case level >= slog.LevelWarn:
		return logtide.LevelWarn
	case level >= slog.LevelInfo:
		return logtide.LevelInfo
	default:
		return logtide.LevelDebug
	}
}

// nestAttr wraps attr in the open group chain (innermost last).
func nestAttr(attr slog.Attr, groups []string) slog.Attr {
	for i := len(groups) - 1; i >= 0; i-- {
		attr = slog.Group(groups[i], attr)
	}
	return attr
}

// groupTarget walks (creating as needed) the nested metadata maps for the
// open group chain and returns the innermost map.
func groupTarget(metadata map[string]any, groups []string) map[string]any {
	target := metadata
	for _, name := range groups {
		next, ok := target[name].(map[string]any)
		if !ok {
			next = make(map[string]any)
			target[name] = next
		}
		target = next
	}
	return target
}

// addAttr resolves attr into target. Error values are collected into errs
// (promoted to structured exceptions) instead of being stored as metadata.
func addAttr(target map[string]any, attr slog.Attr, errs *[]logtide.Exception) {
	value := attr.Value.Resolve()

	if value.Kind() == slog.KindGroup {
		groupAttrs := value.Group()
		if len(groupAttrs) == 0 {
			return
		}
		if attr.Key == "" {
			// inline group: attrs merge into the current level
			for _, ga := range groupAttrs {
				addAttr(target, ga, errs)
			}
			return
		}
		sub, ok := target[attr.Key].(map[string]any)
		if !ok {
			sub = make(map[string]any, len(groupAttrs))
			target[attr.Key] = sub
		}
		for _, ga := range groupAttrs {
			addAttr(sub, ga, errs)
		}
		return
	}

	if attr.Key == "" {
		return
	}

	if err, ok := value.Any().(error); ok && err != nil {
		*errs = append(*errs, logtide.ExceptionsFromError(err)...)
		return
	}

	target[attr.Key] = value.Any()
}
