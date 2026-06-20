// Package otelmetric provides a LogTide integration that bridges OpenTelemetry
// metrics (counters, gauges, histograms) into the LogTide pipeline.
//
// It implements sdkmetric.Exporter, converting each metric data point into a
// logtide.LogEntry sent through the existing Client. Metrics therefore carry the
// same service name, environment, tags and resource attributes as logs and
// traces. It is opt-in: register it only if you need metrics.
//
// Usage:
//
//	integration := otelmetric.New()
//	flush := logtide.Init(logtide.ClientOptions{
//	    DSN:     "https://lp_abc@api.logtide.dev",
//	    Service: "my-service",
//	    Integrations: func(defaults []logtide.Integration) []logtide.Integration {
//	        return append(defaults, integration)
//	    },
//	})
//	defer flush()
//
//	// Register the metric exporter with your MeterProvider:
//	mp := sdkmetric.NewMeterProvider(
//	    sdkmetric.WithReader(sdkmetric.NewPeriodicReader(integration.Exporter())),
//	)
//	otel.SetMeterProvider(mp)
package otelmetric

import (
	"context"
	"encoding/hex"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	logtide "github.com/logtide-dev/logtide-sdk-go"
)

// Exporter implements sdkmetric.Exporter. Each metric data point is converted to
// a logtide.LogEntry and sent via the Client.
type Exporter struct {
	client *logtide.Client
}

// Temporality implements sdkmetric.Exporter. It uses the default selector,
// which reports all instrument kinds as cumulative.
func (e *Exporter) Temporality(kind sdkmetric.InstrumentKind) metricdata.Temporality {
	return sdkmetric.DefaultTemporalitySelector(kind)
}

// Aggregation implements sdkmetric.Exporter using the default aggregation
// selector (Sum for counters, LastValue for gauges, explicit-bucket histogram
// for histograms).
func (e *Exporter) Aggregation(kind sdkmetric.InstrumentKind) sdkmetric.Aggregation {
	return sdkmetric.DefaultAggregationSelector(kind)
}

// Export implements sdkmetric.Exporter. It walks the ResourceMetrics tree and
// emits one LogEntry per data point.
func (e *Exporter) Export(ctx context.Context, rm *metricdata.ResourceMetrics) error {
	if e.client == nil || rm == nil {
		return nil
	}

	var resourceAttrs map[string]any
	if rm.Resource != nil {
		resourceAttrs = iterToMap(rm.Resource.Iter())
	}

	for _, sm := range rm.ScopeMetrics {
		var scopeMeta map[string]any
		if sm.Scope.Name != "" || sm.Scope.Version != "" {
			scopeMeta = map[string]any{"name": sm.Scope.Name}
			if sm.Scope.Version != "" {
				scopeMeta["version"] = sm.Scope.Version
			}
		}

		for _, m := range sm.Metrics {
			for _, metric := range metricToEntries(m) {
				meta := map[string]any{"metric": metric.meta}
				if resourceAttrs != nil {
					meta["resource"] = resourceAttrs
				}
				if scopeMeta != nil {
					meta["scope"] = scopeMeta
				}

				// When a data point carries an exemplar with trace context,
				// link the entry to that trace/span. Use context.Background()
				// as the base so no ambient span in ctx overrides it (the
				// client extracts span IDs before applying the scope).
				entryCtx := ctx
				if metric.traceID != "" {
					scope := logtide.NewScope(0)
					scope.SetTraceContext(metric.traceID, metric.spanID)
					entryCtx = logtide.WithScope(context.Background(), scope)
				}

				e.client.Info(entryCtx, metric.message, meta)
			}
		}
	}
	return nil
}

// ForceFlush implements sdkmetric.Exporter.
func (e *Exporter) ForceFlush(ctx context.Context) error {
	if e.client != nil {
		e.client.Flush(ctx)
	}
	return nil
}

// Shutdown implements sdkmetric.Exporter.
func (e *Exporter) Shutdown(ctx context.Context) error {
	if e.client != nil {
		e.client.Flush(ctx)
	}
	return nil
}

// Integration implements logtide.Integration and holds the Exporter reference.
// Call Exporter() after Setup has run to obtain the configured exporter.
type Integration struct {
	exporter *Exporter
}

// New creates an Integration. Register it in ClientOptions.Integrations, then
// call Exporter() after Init to obtain the configured sdkmetric.Exporter.
func New() *Integration {
	return &Integration{exporter: &Exporter{}}
}

// Name implements logtide.Integration.
func (i *Integration) Name() string { return "OTelMetricExport" }

// Setup implements logtide.Integration. Stores the client in the Exporter.
func (i *Integration) Setup(client *logtide.Client) {
	i.exporter.client = client
}

// Exporter returns the sdkmetric.Exporter backed by this integration's Client.
func (i *Integration) Exporter() *Exporter {
	return i.exporter
}

// --- metric → LogEntry conversion ---

// entry is the intermediate result of converting a single data point.
type entry struct {
	message string
	meta    map[string]any
	// traceID/spanID are taken from the data point's most recent exemplar
	// (empty when the point carries no trace-linked exemplar).
	traceID string
	spanID  string
}

// metricToEntries converts one Metrics aggregation into one entry per data point.
func metricToEntries(m metricdata.Metrics) []entry {
	switch data := m.Data.(type) {
	case metricdata.Sum[int64]:
		return sumEntries(m, data.DataPoints, data.IsMonotonic)
	case metricdata.Sum[float64]:
		return sumEntries(m, data.DataPoints, data.IsMonotonic)
	case metricdata.Gauge[int64]:
		return gaugeEntries(m, data.DataPoints)
	case metricdata.Gauge[float64]:
		return gaugeEntries(m, data.DataPoints)
	case metricdata.Histogram[int64]:
		return histogramEntries(m, data.DataPoints)
	case metricdata.Histogram[float64]:
		return histogramEntries(m, data.DataPoints)
	default:
		return nil
	}
}

func sumEntries[N int64 | float64](m metricdata.Metrics, dps []metricdata.DataPoint[N], monotonic bool) []entry {
	out := make([]entry, 0, len(dps))
	for _, dp := range dps {
		meta := baseMeta(m, "counter", dp.Attributes, dp.StartTime, dp.Time)
		meta["value"] = dp.Value
		meta["is_monotonic"] = monotonic
		e := entry{
			message: fmt.Sprintf("metric %s = %v", m.Name, dp.Value),
			meta:    meta,
		}
		applyExemplars(&e, dp.Exemplars)
		out = append(out, e)
	}
	return out
}

func gaugeEntries[N int64 | float64](m metricdata.Metrics, dps []metricdata.DataPoint[N]) []entry {
	out := make([]entry, 0, len(dps))
	for _, dp := range dps {
		meta := baseMeta(m, "gauge", dp.Attributes, dp.StartTime, dp.Time)
		meta["value"] = dp.Value
		e := entry{
			message: fmt.Sprintf("metric %s = %v", m.Name, dp.Value),
			meta:    meta,
		}
		applyExemplars(&e, dp.Exemplars)
		out = append(out, e)
	}
	return out
}

func histogramEntries[N int64 | float64](m metricdata.Metrics, dps []metricdata.HistogramDataPoint[N]) []entry {
	out := make([]entry, 0, len(dps))
	for _, dp := range dps {
		meta := baseMeta(m, "histogram", dp.Attributes, dp.StartTime, dp.Time)
		meta["count"] = dp.Count
		meta["sum"] = dp.Sum
		meta["bucket_counts"] = dp.BucketCounts
		meta["explicit_bounds"] = dp.Bounds
		if v, ok := dp.Min.Value(); ok {
			meta["min"] = v
		}
		if v, ok := dp.Max.Value(); ok {
			meta["max"] = v
		}
		e := entry{
			message: fmt.Sprintf("metric %s count=%d sum=%v", m.Name, dp.Count, dp.Sum),
			meta:    meta,
		}
		applyExemplars(&e, dp.Exemplars)
		out = append(out, e)
	}
	return out
}

// applyExemplars records the data point's exemplars in metadata and links the
// entry to the trace/span of the most recent trace-bearing exemplar. Exemplars
// are only populated when an exemplar filter is configured and a sampled span is
// active during measurement, so this is a no-op for the common case.
func applyExemplars[N int64 | float64](e *entry, exemplars []metricdata.Exemplar[N]) {
	if len(exemplars) == 0 {
		return
	}
	list := make([]map[string]any, 0, len(exemplars))
	for _, ex := range exemplars {
		em := map[string]any{"value": ex.Value}
		if !ex.Time.IsZero() {
			em["time"] = ex.Time.Format(time.RFC3339Nano)
		}
		if len(ex.TraceID) > 0 {
			tid := hex.EncodeToString(ex.TraceID)
			em["trace_id"] = tid
			e.traceID = tid
		}
		if len(ex.SpanID) > 0 {
			sid := hex.EncodeToString(ex.SpanID)
			em["span_id"] = sid
			e.spanID = sid
		}
		if len(ex.FilteredAttributes) > 0 {
			attrs := make(map[string]any, len(ex.FilteredAttributes))
			for _, a := range ex.FilteredAttributes {
				attrs[string(a.Key)] = a.Value.AsInterface()
			}
			em["filtered_attributes"] = attrs
		}
		list = append(list, em)
	}
	e.meta["exemplars"] = list
}

// baseMeta builds the common metric metadata shared by every data-point type.
func baseMeta(m metricdata.Metrics, typ string, attrs attribute.Set, start, ts time.Time) map[string]any {
	meta := map[string]any{
		"name": m.Name,
		"type": typ,
	}
	if m.Description != "" {
		meta["description"] = m.Description
	}
	if m.Unit != "" {
		meta["unit"] = m.Unit
	}
	if attrs.Len() > 0 {
		meta["attributes"] = iterToMap(attrs.Iter())
	}
	if !start.IsZero() {
		meta["start_time"] = start.Format(time.RFC3339Nano)
	}
	if !ts.IsZero() {
		meta["time"] = ts.Format(time.RFC3339Nano)
	}
	return meta
}

// iterToMap collects an attribute.Iterator into a plain map.
func iterToMap(it attribute.Iterator) map[string]any {
	m := make(map[string]any, it.Len())
	for it.Next() {
		kv := it.Attribute()
		m[string(kv.Key)] = kv.Value.AsInterface()
	}
	return m
}
