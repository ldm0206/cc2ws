// internal/core/config.go
package core

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"time"
)

type Config struct {
	Listen                string
	UpstreamBase          string
	UpstreamWS            string
	InsecureSkipTLSVerify bool
	ConnectTimeout        time.Duration
	IdleTimeout           time.Duration
	LogLevel              string
}

func swapScheme(base string) (string, error) {
	u, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("parse upstream base: %w", err)
	}
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	case "http":
		u.Scheme = "ws"
	default:
		return "", fmt.Errorf("upstream base must be http(s)://, got scheme %q", u.Scheme)
	}
	if u.Host == "" {
		return "", fmt.Errorf("upstream base missing host")
	}
	u.Path = ""
	u.RawPath = ""
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}

func LoadConfig() (Config, error) {
	base := envOr("UPSTREAM_BASE", "")
	if base == "" {
		return Config{}, fmt.Errorf("UPSTREAM_BASE is required")
	}
	ws, err := swapScheme(base)
	if err != nil {
		return Config{}, err
	}
	ct, err := time.ParseDuration(envOr("CONNECT_TIMEOUT", "10s"))
	if err != nil {
		return Config{}, fmt.Errorf("CONNECT_TIMEOUT: %w", err)
	}
	it, err := time.ParseDuration(envOr("IDLE_TIMEOUT", "600s"))
	if err != nil {
		return Config{}, fmt.Errorf("IDLE_TIMEOUT: %w", err)
	}
	skip, err := strconv.ParseBool(envOr("UPSTREAM_INSECURE_SKIP_TLS_VERIFY", "false"))
	if err != nil {
		return Config{}, fmt.Errorf("UPSTREAM_INSECURE_SKIP_TLS_VERIFY: %w", err)
	}
	return Config{
		Listen:                envOr("LISTEN", "127.0.0.1:18080"),
		UpstreamBase:          base,
		UpstreamWS:            ws,
		InsecureSkipTLSVerify: skip,
		ConnectTimeout:        ct,
		IdleTimeout:           it,
		LogLevel:              envOr("LOG_LEVEL", "info"),
	}, nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
