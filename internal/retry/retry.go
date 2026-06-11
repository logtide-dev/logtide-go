// Package retry provides exponential-backoff retry logic for HTTP calls.
package retry

import (
	"context"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net/http"
	"strconv"
	"time"
)

// Config holds parameters for the retry strategy.
type Config struct {
	MaxRetries int
	MinBackoff time.Duration
	MaxBackoff time.Duration
}

// fn is a function that makes an HTTP call and can be retried.
type fn func(ctx context.Context) (*http.Response, error)

// Do executes f with exponential-backoff retry according to cfg.
// It retries on network errors and on HTTP 408, 429, 500, 502, 503, 504.
// A Retry-After header (delta-seconds) on the response overrides the
// computed backoff delay (spec 002 §6).
func Do(ctx context.Context, cfg Config, f fn) (*http.Response, error) {
	var (
		resp *http.Response
		err  error
	)

	for attempt := 0; attempt <= cfg.MaxRetries; attempt++ {
		resp, err = f(ctx)

		if !shouldRetry(resp, err) {
			return resp, err
		}

		if attempt == cfg.MaxRetries {
			if err != nil {
				return nil, fmt.Errorf("max retries exceeded: %w", err)
			}
			// Retryable HTTP status but out of attempts: drain and close body,
			// then return an error so callers need not inspect StatusCode.
			if resp != nil && resp.Body != nil {
				io.Copy(io.Discard, resp.Body) //nolint:errcheck
				resp.Body.Close()             //nolint:errcheck
			}
			return nil, fmt.Errorf("max retries exceeded: server returned status %d", resp.StatusCode)
		}

		// Capture Retry-After before the body is drained: the server's
		// requested delay overrides the computed backoff.
		wait := calculateBackoff(attempt, cfg)
		if resp != nil {
			if ra, ok := parseRetryAfter(resp.Header.Get("Retry-After")); ok {
				wait = ra
			}
		}

		// Drain and close the body before sleeping so the underlying TCP
		// connection is returned to the pool rather than discarded.
		if resp != nil && resp.Body != nil {
			io.Copy(io.Discard, resp.Body) //nolint:errcheck
			resp.Body.Close()             //nolint:errcheck
			resp = nil
		}

		timer := time.NewTimer(wait)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		}
	}

	return resp, err
}

func shouldRetry(resp *http.Response, err error) bool {
	if err != nil {
		return true
	}
	if resp == nil {
		return false
	}
	switch resp.StatusCode {
	case 408, 429, 500, 502, 503, 504:
		return true
	default:
		return false
	}
}

// parseRetryAfter parses a Retry-After header value in delta-seconds form.
func parseRetryAfter(header string) (time.Duration, bool) {
	if header == "" {
		return 0, false
	}
	seconds, err := strconv.ParseFloat(header, 64)
	if err != nil || seconds < 0 {
		return 0, false
	}
	return time.Duration(seconds * float64(time.Second)), true
}

func calculateBackoff(attempt int, cfg Config) time.Duration {
	backoff := float64(cfg.MinBackoff) * math.Pow(2, float64(attempt))
	if backoff > float64(cfg.MaxBackoff) {
		backoff = float64(cfg.MaxBackoff)
	}
	jitter := rand.Float64() * 0.25 * backoff
	return time.Duration(backoff + jitter)
}
