package main

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"time"
)

// Config holds proxy configuration merged from environment (and CLI in main.go).
type Config struct {
	Listen                string
	UpstreamBase          string // origin as given, e.g. https://host
	UpstreamWS            string // scheme-swapped origin, e.g. wss://host
	InsecureSkipTLSVerify bool
	ConnectTimeout        time.Duration
	IdleTimeout           time.Duration
	LogLevel              string
}

// swapScheme converts an http(s):// origin to a ws(s):// origin, stripping any
// path/query (UPSTREAM_BASE is an origin only). scheme rule: https→wss, http→ws.
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

// LoadConfig reads environment variables and validates them.
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
