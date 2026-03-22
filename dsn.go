package logtide

import (
	"fmt"
	"net/url"
)

// DSN holds the parsed components of a LogTide Data Source Name.
type DSN struct {
	APIKey string
	Scheme string // "https" or "http"
	Host   string // host[:port]
	Path   string // optional sub-path prefix (without trailing slash)
}

// ParseDSN parses a DSN string of the form:
//
//	https://{api_key}@{host}[/{path}]
//
// Example:
//
//	https://lp_abc123@api.logtide.dev
func ParseDSN(rawDSN string) (*DSN, error) {
	u, err := url.Parse(rawDSN)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalidDSN, err.Error())
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return nil, fmt.Errorf("%w: scheme must be http or https, got %q", ErrInvalidDSN, u.Scheme)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("%w: missing host", ErrInvalidDSN)
	}
	if u.User == nil || u.User.Username() == "" {
		return nil, fmt.Errorf("%w: missing API key in userinfo (expected https://apikey@host)", ErrInvalidDSN)
	}

	path := u.Path
	if path == "/" {
		path = ""
	}

	return &DSN{
		APIKey: u.User.Username(),
		Scheme: u.Scheme,
		Host:   u.Host,
		Path:   path,
	}, nil
}

// IngestURL returns the full URL for the log ingest endpoint.
func (d *DSN) IngestURL() string {
	return fmt.Sprintf("%s://%s%s/api/v1/ingest", d.Scheme, d.Host, d.Path)
}

