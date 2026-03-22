package retry_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/logtide-dev/logtide-sdk-go/internal/retry"
)

func TestSuccessOnFirstAttempt(t *testing.T) {
	attempts := 0
	cfg := retry.Config{MaxRetries: 3, MinBackoff: 5 * time.Millisecond, MaxBackoff: 50 * time.Millisecond}
	resp, err := retry.Do(context.Background(), cfg, func(_ context.Context) (*http.Response, error) {
		attempts++
		return &http.Response{StatusCode: 200}, nil
	})
	if err != nil || resp.StatusCode != 200 || attempts != 1 {
		t.Errorf("got err=%v status=%d attempts=%d, want nil/200/1", err, resp.StatusCode, attempts)
	}
}

func TestRetriesOnServerError(t *testing.T) {
	attempts := 0
	cfg := retry.Config{MaxRetries: 2, MinBackoff: 5 * time.Millisecond, MaxBackoff: 50 * time.Millisecond}
	resp, err := retry.Do(context.Background(), cfg, func(_ context.Context) (*http.Response, error) {
		attempts++
		if attempts <= 2 {
			return &http.Response{StatusCode: 500}, nil
		}
		return &http.Response{StatusCode: 200}, nil
	})
	if err != nil || resp.StatusCode != 200 || attempts != 3 {
		t.Errorf("got err=%v status=%d attempts=%d, want nil/200/3", err, resp.StatusCode, attempts)
	}
}

func TestExhaustsRetries(t *testing.T) {
	attempts := 0
	cfg := retry.Config{MaxRetries: 2, MinBackoff: 5 * time.Millisecond, MaxBackoff: 50 * time.Millisecond}
	resp, err := retry.Do(context.Background(), cfg, func(_ context.Context) (*http.Response, error) {
		attempts++
		return &http.Response{StatusCode: 500, Body: http.NoBody}, nil
	})
	if err == nil {
		t.Error("expected error after exhausting retries on 5xx, got nil")
	}
	if resp != nil {
		t.Errorf("expected nil resp after exhausting retries, got status=%d", resp.StatusCode)
	}
	if attempts != 3 {
		t.Errorf("attempts = %d, want 3", attempts)
	}
}

func TestRetriesOnNetworkError(t *testing.T) {
	attempts := 0
	cfg := retry.Config{MaxRetries: 2, MinBackoff: 5 * time.Millisecond, MaxBackoff: 50 * time.Millisecond}
	_, err := retry.Do(context.Background(), cfg, func(_ context.Context) (*http.Response, error) {
		attempts++
		return nil, errors.New("network error")
	})
	if err == nil {
		t.Error("expected error, got nil")
	}
	if attempts != 3 {
		t.Errorf("attempts = %d, want 3", attempts)
	}
}

func TestContextCancellation(t *testing.T) {
	attempts := 0
	cfg := retry.Config{MaxRetries: 5, MinBackoff: 100 * time.Millisecond, MaxBackoff: time.Second}
	ctx, cancel := context.WithCancel(context.Background())

	_, err := retry.Do(ctx, cfg, func(ctx context.Context) (*http.Response, error) {
		attempts++
		if attempts == 2 {
			cancel()
		}
		return &http.Response{StatusCode: 500}, nil
	})

	if err == nil {
		t.Error("expected error after cancellation")
	}
	if attempts > 2 {
		t.Errorf("attempts = %d after cancel, want <= 2", attempts)
	}
}

func TestNoRetryOn400(t *testing.T) {
	attempts := 0
	cfg := retry.Config{MaxRetries: 3, MinBackoff: 5 * time.Millisecond, MaxBackoff: 50 * time.Millisecond}
	resp, err := retry.Do(context.Background(), cfg, func(_ context.Context) (*http.Response, error) {
		attempts++
		return &http.Response{StatusCode: 400}, nil
	})
	if err != nil || resp.StatusCode != 400 || attempts != 1 {
		t.Errorf("got err=%v status=%d attempts=%d, want nil/400/1", err, resp.StatusCode, attempts)
	}
}

func TestAllRetryableStatusCodes(t *testing.T) {
	retryable := []int{429, 500, 502, 503, 504}
	for _, code := range retryable {
		code := code
		t.Run(fmt.Sprintf("status_%d", code), func(t *testing.T) {
			attempts := 0
			cfg := retry.Config{MaxRetries: 1, MinBackoff: 5 * time.Millisecond, MaxBackoff: 10 * time.Millisecond}
			retry.Do(context.Background(), cfg, func(_ context.Context) (*http.Response, error) {
				attempts++
				return &http.Response{StatusCode: code, Body: http.NoBody}, nil
			})
			if attempts != 2 {
				t.Errorf("status %d: attempts = %d, want 2 (initial + 1 retry)", code, attempts)
			}
		})
	}
}

func TestNilResponseNotRetried(t *testing.T) {
	// A nil response with no error should NOT be retried (shouldRetry returns false).
	attempts := 0
	cfg := retry.Config{MaxRetries: 3, MinBackoff: 5 * time.Millisecond, MaxBackoff: 10 * time.Millisecond}
	resp, err := retry.Do(context.Background(), cfg, func(_ context.Context) (*http.Response, error) {
		attempts++
		return nil, nil
	})
	if resp != nil {
		t.Errorf("resp = %v, want nil", resp)
	}
	if err != nil {
		t.Errorf("err = %v, want nil", err)
	}
	if attempts != 1 {
		t.Errorf("attempts = %d, want 1 (nil response with no error should not retry)", attempts)
	}
}
