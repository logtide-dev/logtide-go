package otelexport_test

import (
	"context"
	"testing"

	logtide "github.com/logtide-dev/logtide-sdk-go"
	"github.com/logtide-dev/logtide-sdk-go/integrations/otelexport"
)

func TestNewCreatesIntegration(t *testing.T) {
	i := otelexport.New()
	if i == nil {
		t.Fatal("New() returned nil")
	}
	if i.Name() != "OTelSpanExport" {
		t.Errorf("Name() = %q, want OTelSpanExport", i.Name())
	}
}

func TestExporterBeforeSetupHasNilClient(t *testing.T) {
	i := otelexport.New()
	// Before Setup is called, exporter has a nil client.
	// ExportSpans with a nil client should return nil (no panic).
	if err := i.Exporter().ExportSpans(context.Background(), nil); err != nil {
		t.Errorf("ExportSpans with nil client = %v, want nil", err)
	}
}

func TestExporterShutdownWithNilClient(t *testing.T) {
	i := otelexport.New()
	// Shutdown before Setup should not panic.
	if err := i.Exporter().Shutdown(context.Background()); err != nil {
		t.Errorf("Shutdown with nil client = %v, want nil", err)
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

	i := otelexport.New()
	i.Setup(client)

	// After Setup, Shutdown should flush the client without panicking.
	if err := i.Exporter().Shutdown(context.Background()); err != nil {
		t.Errorf("Shutdown after Setup = %v, want nil", err)
	}
}

func TestIntegrationRegisteredViaClientOptions(t *testing.T) {
	otelInt := otelexport.New()
	client, err := logtide.NewClient(logtide.ClientOptions{
		Service:   "test",
		Transport: logtide.NoopTransport{},
		Integrations: func(defaults []logtide.Integration) []logtide.Integration {
			return append(defaults, otelInt)
		},
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()

	// After NewClient, the integration's Setup has been called.
	// ExportSpans with empty span list should succeed.
	if err := otelInt.Exporter().ExportSpans(context.Background(), nil); err != nil {
		t.Errorf("ExportSpans after init = %v, want nil", err)
	}
}
