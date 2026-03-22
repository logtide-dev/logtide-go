package logtide_test

import (
	"errors"
	"testing"

	logtide "github.com/logtide-dev/logtide-sdk-go"
)

func TestValidationError(t *testing.T) {
	err := &logtide.ValidationError{Field: "service", Message: "required"}

	if err.Error() == "" {
		t.Fatal("ValidationError.Error() is empty")
	}

	var target *logtide.ValidationError
	if !errors.As(err, &target) {
		t.Error("errors.As should match *ValidationError")
	}
}

func TestHTTPError(t *testing.T) {
	tests := []struct {
		name    string
		err     *logtide.HTTPError
		wantMsg string
	}{
		{"with message", &logtide.HTTPError{StatusCode: 429, Message: "rate limit"}, "429"},
		{"without message", &logtide.HTTPError{StatusCode: 503}, "503"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if msg := tt.err.Error(); msg == "" {
				t.Errorf("HTTPError.Error() is empty")
			}

			var target *logtide.HTTPError
			if !errors.As(tt.err, &target) {
				t.Error("errors.As should match *HTTPError")
			}
		})
	}
}

func TestSentinelErrors(t *testing.T) {
	sentinels := []error{
		logtide.ErrClientClosed,
		logtide.ErrCircuitOpen,
		logtide.ErrInvalidDSN,
		logtide.ErrServiceRequired,
	}
	for _, err := range sentinels {
		if err == nil {
			t.Errorf("sentinel error is nil")
		}
		if !errors.Is(err, err) {
			t.Errorf("errors.Is failed for %v", err)
		}
	}
}
