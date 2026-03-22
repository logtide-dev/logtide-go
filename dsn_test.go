package logtide_test

import (
	"testing"

	logtide "github.com/logtide-dev/logtide-sdk-go"
)

func TestParseDSN(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
		apiKey  string
		host    string
		url     string
	}{
		{
			name:   "standard https",
			input:  "https://lp_abc123@api.logtide.dev",
			apiKey: "lp_abc123",
			host:   "api.logtide.dev",
			url:    "https://api.logtide.dev/api/v1/ingest",
		},
		{
			name:   "http with path",
			input:  "http://key@localhost:9000/v2",
			apiKey: "key",
			host:   "localhost:9000",
			url:    "http://localhost:9000/v2/api/v1/ingest",
		},
		{
			name:    "missing api key",
			input:   "https://api.logtide.dev",
			wantErr: true,
		},
		{
			name:    "missing host",
			input:   "https://key@",
			wantErr: true,
		},
		{
			name:    "wrong scheme",
			input:   "ftp://key@host",
			wantErr: true,
		},
		{
			name:    "not a url",
			input:   "not-a-dsn",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dsn, err := logtide.ParseDSN(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseDSN(%q) err = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if dsn.APIKey != tt.apiKey {
				t.Errorf("APIKey = %q, want %q", dsn.APIKey, tt.apiKey)
			}
			if dsn.Host != tt.host {
				t.Errorf("Host = %q, want %q", dsn.Host, tt.host)
			}
			if got := dsn.IngestURL(); got != tt.url {
				t.Errorf("IngestURL() = %q, want %q", got, tt.url)
			}
		})
	}
}
