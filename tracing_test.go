package logtide_test

import (
	"testing"

	logtide "github.com/logtide-dev/logtide-sdk-go"
)

func TestParseTraceparent(t *testing.T) {
	tests := []struct {
		name    string
		header  string
		traceID string
		spanID  string
		sampled bool
		wantErr bool
	}{
		{
			name:    "sampled",
			header:  "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
			traceID: "4bf92f3577b34da6a3ce929d0e0e4736",
			spanID:  "00f067aa0ba902b7",
			sampled: true,
		},
		{
			name:    "not sampled",
			header:  "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-00",
			traceID: "4bf92f3577b34da6a3ce929d0e0e4736",
			spanID:  "00f067aa0ba902b7",
			sampled: false,
		},
		{
			name:    "malformed",
			header:  "invalid-header",
			wantErr: true,
		},
		{
			name:    "wrong version",
			header:  "01-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			traceID, spanID, sampled, err := logtide.ParseTraceparent(tt.header)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseTraceparent(%q) err = %v, wantErr %v", tt.header, err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if traceID != tt.traceID {
				t.Errorf("traceID = %q, want %q", traceID, tt.traceID)
			}
			if spanID != tt.spanID {
				t.Errorf("spanID = %q, want %q", spanID, tt.spanID)
			}
			if sampled != tt.sampled {
				t.Errorf("sampled = %v, want %v", sampled, tt.sampled)
			}
		})
	}
}

func TestFormatTraceparent(t *testing.T) {
	got := logtide.FormatTraceparent("4bf92f3577b34da6a3ce929d0e0e4736", "00f067aa0ba902b7", true)
	want := "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	if got != want {
		t.Errorf("FormatTraceparent() = %q, want %q", got, want)
	}

	got2 := logtide.FormatTraceparent("4bf92f3577b34da6a3ce929d0e0e4736", "00f067aa0ba902b7", false)
	if got2[len(got2)-2:] != "00" {
		t.Errorf("unsampled flags = %q, want 00", got2[len(got2)-2:])
	}
}
