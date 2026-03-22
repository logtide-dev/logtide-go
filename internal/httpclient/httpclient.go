// Package httpclient provides a thin HTTP client with LogTide-specific
// authentication and connection-pool configuration.
package httpclient

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

const defaultTimeout = 30 * time.Second

// Client wraps net/http.Client with LogTide authentication headers.
type Client struct {
	inner   *http.Client
	apiKey  string
	version string
}

// Options configures the HTTP client.
type Options struct {
	Timeout         time.Duration
	MaxIdleConns    int
	IdleConnTimeout time.Duration
	TLSMinVersion   uint16
	// Version is included in the User-Agent header (e.g. "1.0.0").
	Version string
	// Inner, if non-nil, is used as the underlying *http.Client instead of
	// constructing a new one. Transport and Timeout of the supplied client
	// take precedence over the other options.
	Inner *http.Client
}

// New creates a Client with the given API key and options.
// If opts.Inner is non-nil it is used as-is and the other transport options are ignored.
func New(apiKey string, opts Options) *Client {
	if opts.Version == "" {
		opts.Version = "unknown"
	}

	var inner *http.Client
	if opts.Inner != nil {
		inner = opts.Inner
	} else {
		if opts.Timeout <= 0 {
			opts.Timeout = defaultTimeout
		}
		if opts.MaxIdleConns <= 0 {
			opts.MaxIdleConns = 10
		}
		if opts.IdleConnTimeout <= 0 {
			opts.IdleConnTimeout = 90 * time.Second
		}
		if opts.TLSMinVersion == 0 {
			opts.TLSMinVersion = tls.VersionTLS12
		}

		transport := &http.Transport{
			MaxIdleConns:        opts.MaxIdleConns,
			MaxIdleConnsPerHost: opts.MaxIdleConns,
			IdleConnTimeout:     opts.IdleConnTimeout,
			TLSClientConfig:     &tls.Config{MinVersion: opts.TLSMinVersion},
			DialContext: (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
		}
		inner = &http.Client{
			Transport: transport,
			Timeout:   opts.Timeout,
		}
	}

	return &Client{
		inner:   inner,
		apiKey:  apiKey,
		version: opts.Version,
	}
}

// Post sends a JSON-encoded POST request to url and returns the raw response.
// The caller is responsible for closing resp.Body.
func (c *Client) Post(ctx context.Context, url string, payload any) (*http.Response, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("httpclient: marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("httpclient: create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", c.apiKey)
	req.Header.Set("User-Agent", "logtide-sdk-go/"+c.version)

	resp, err := c.inner.Do(req)
	if err != nil {
		return nil, fmt.Errorf("httpclient: send request: %w", err)
	}
	return resp, nil
}

// ReadBody reads and closes the response body, returning it as a string.
func ReadBody(resp *http.Response) (string, error) {
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("httpclient: read body: %w", err)
	}
	return string(body), nil
}
