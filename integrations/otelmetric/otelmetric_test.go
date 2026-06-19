package otelmetric_test

import (
	"context"
	"sync"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/resource"

	logtide "github.com/logtide-dev/logtide-sdk-go"
	"github.com/logtide-dev/logtide-sdk-go/integrations/otelmetric"
)

// captureTransport records every LogEntry the client dispatches.
type captureTransport struct {
	mu      sync.Mutex
	entries []logtide.LogEntry
}

func (t *captureTransport) Send(e *logtide.LogEntry) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.entries = append(t.entries, *e)
}

func (t *captureTransport) Flush(context.Context) bool { return true }
func (t *captureTransport) Close()                     {}

func (t *captureTransport) snapshot() []logtide.LogEntry {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]logtide.LogEntry, len(t.entries))
	copy(out, t.entries)
	return out
}

func TestNewCreatesIntegration(t *testing.T) {
	i := otelmetric.New()
	if i == nil {
		t.Fatal("New() returned nil")
	}
	if i.Name() != "OTelMetricExport" {
		t.Errorf("Name() = %q, want OTelMetricExport", i.Name())
	}
}

func TestExporterBeforeSetupHasNilClient(t *testing.T) {
	i := otelmetric.New()
	// Before Setup, the exporter has a nil client: Export must be a no-op.
	if err := i.Exporter().Export(context.Background(), &metricdata.ResourceMetrics{}); err != nil {
		t.Errorf("Export with nil client = %v, want nil", err)
	}
}

func TestExporterShutdownWithNilClient(t *testing.T) {
	i := otelmetric.New()
	if err := i.Exporter().Shutdown(context.Background()); err != nil {
		t.Errorf("Shutdown with nil client = %v, want nil", err)
	}
}

func TestExporterForceFlushWithNilClient(t *testing.T) {
	i := otelmetric.New()
	if err := i.Exporter().ForceFlush(context.Background()); err != nil {
		t.Errorf("ForceFlush with nil client = %v, want nil", err)
	}
}

func TestSetupWiresClientToExporter(t *testing.T) {
	client, err := logtide.NewClient(logtide.ClientOptions{
		Service:   "test",
		Transport: logtide.NoopTransport{},
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()

	i := otelmetric.New()
	i.Setup(client)

	if err := i.Exporter().Shutdown(context.Background()); err != nil {
		t.Errorf("Shutdown after Setup = %v, want nil", err)
	}
}

func TestIntegrationRegisteredViaClientOptions(t *testing.T) {
	mInt := otelmetric.New()
	client, err := logtide.NewClient(logtide.ClientOptions{
		Service:   "test",
		Transport: logtide.NoopTransport{},
		Integrations: func(defaults []logtide.Integration) []logtide.Integration {
			return append(defaults, mInt)
		},
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()

	// After NewClient, Setup has run, so Export of empty metrics succeeds.
	if err := mInt.Exporter().Export(context.Background(), &metricdata.ResourceMetrics{}); err != nil {
		t.Errorf("Export after init = %v, want nil", err)
	}
}

func TestTemporalityIsCumulative(t *testing.T) {
	i := otelmetric.New()
	got := i.Exporter().Temporality(sdkmetric.InstrumentKindCounter)
	if got != metricdata.CumulativeTemporality {
		t.Errorf("Temporality = %v, want Cumulative", got)
	}
}

func TestAggregationIsDefault(t *testing.T) {
	i := otelmetric.New()
	// Default selector maps a Counter to a Sum aggregation.
	got := i.Exporter().Aggregation(sdkmetric.InstrumentKindCounter)
	if _, ok := got.(sdkmetric.AggregationSum); !ok {
		t.Errorf("Aggregation for Counter = %T, want AggregationSum", got)
	}
}

func TestExportConvertsEachDataPointToEntry(t *testing.T) {
	tr := &captureTransport{}
	client, err := logtide.NewClient(logtide.ClientOptions{
		Service:   "metrics-test",
		Transport: tr,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()

	i := otelmetric.New()
	i.Setup(client)

	rm := &metricdata.ResourceMetrics{
		Resource: resource.NewSchemaless(attribute.String("host.name", "box-1")),
		ScopeMetrics: []metricdata.ScopeMetrics{
			{
				Metrics: []metricdata.Metrics{
					{
						Name:        "http.requests",
						Description: "request count",
						Unit:        "1",
						Data: metricdata.Sum[int64]{
							IsMonotonic: true,
							Temporality: metricdata.CumulativeTemporality,
							DataPoints: []metricdata.DataPoint[int64]{
								{Value: 42, Attributes: attribute.NewSet(attribute.String("method", "GET"))},
							},
						},
					},
					{
						Name: "cpu.usage",
						Unit: "%",
						Data: metricdata.Gauge[float64]{
							DataPoints: []metricdata.DataPoint[float64]{
								{Value: 3.5},
							},
						},
					},
					{
						Name: "http.latency",
						Unit: "ms",
						Data: metricdata.Histogram[float64]{
							Temporality: metricdata.CumulativeTemporality,
							DataPoints: []metricdata.HistogramDataPoint[float64]{
								{
									Count:        2,
									Sum:          10,
									Bounds:       []float64{1, 5},
									BucketCounts: []uint64{0, 1, 1},
									Min:          metricdata.NewExtrema[float64](2),
									Max:          metricdata.NewExtrema[float64](8),
								},
							},
						},
					},
				},
			},
		},
	}

	if err := i.Exporter().Export(context.Background(), rm); err != nil {
		t.Fatalf("Export: %v", err)
	}

	entries := tr.snapshot()
	if len(entries) != 3 {
		t.Fatalf("got %d entries, want 3 (one per data point)", len(entries))
	}

	byName := map[string]logtide.LogEntry{}
	for _, e := range entries {
		if e.Level != logtide.LevelInfo {
			t.Errorf("entry level = %q, want info", e.Level)
		}
		m, ok := e.Metadata["metric"].(map[string]any)
		if !ok {
			t.Fatalf("entry metadata missing metric map: %#v", e.Metadata)
		}
		name, _ := m["name"].(string)
		byName[name] = e
	}

	// Counter
	counter := byName["http.requests"].Metadata["metric"].(map[string]any)
	if counter["type"] != "counter" {
		t.Errorf("counter type = %v, want counter", counter["type"])
	}
	if counter["value"] != int64(42) {
		t.Errorf("counter value = %v (%T), want int64(42)", counter["value"], counter["value"])
	}
	attrs, _ := counter["attributes"].(map[string]any)
	if attrs["method"] != "GET" {
		t.Errorf("counter attributes.method = %v, want GET", attrs["method"])
	}

	// Gauge
	gauge := byName["cpu.usage"].Metadata["metric"].(map[string]any)
	if gauge["type"] != "gauge" {
		t.Errorf("gauge type = %v, want gauge", gauge["type"])
	}
	if gauge["value"] != float64(3.5) {
		t.Errorf("gauge value = %v, want 3.5", gauge["value"])
	}

	// Histogram
	hist := byName["http.latency"].Metadata["metric"].(map[string]any)
	if hist["type"] != "histogram" {
		t.Errorf("histogram type = %v, want histogram", hist["type"])
	}
	if hist["count"] != uint64(2) {
		t.Errorf("histogram count = %v, want 2", hist["count"])
	}
	if hist["sum"] != float64(10) {
		t.Errorf("histogram sum = %v, want 10", hist["sum"])
	}
	if hist["min"] != float64(2) {
		t.Errorf("histogram min = %v, want 2", hist["min"])
	}
	if hist["max"] != float64(8) {
		t.Errorf("histogram max = %v, want 8", hist["max"])
	}

	// Resource attributes flow into metadata.
	res, ok := byName["cpu.usage"].Metadata["resource"].(map[string]any)
	if !ok || res["host.name"] != "box-1" {
		t.Errorf("resource attributes not propagated: %#v", byName["cpu.usage"].Metadata["resource"])
	}
}
