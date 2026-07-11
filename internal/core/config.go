// internal/core/config.go
package core

import (
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
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
	fc := LoadFile()

	// env wins over file; file wins over defaults.
	base := envOr("UPSTREAM_BASE", pickStr(fc.UpstreamBase, ""))
	if base == "" {
		return Config{}, fmt.Errorf("UPSTREAM_BASE is required")
	}
	ws, err := swapScheme(base)
	if err != nil {
		return Config{}, err
	}
	ct, err := time.ParseDuration(envOr("CONNECT_TIMEOUT", pickStr(fc.ConnectTimeout, "10s")))
	if err != nil {
		return Config{}, fmt.Errorf("CONNECT_TIMEOUT: %w", err)
	}
	it, err := time.ParseDuration(envOr("IDLE_TIMEOUT", pickStr(fc.IdleTimeout, "600s")))
	if err != nil {
		return Config{}, fmt.Errorf("IDLE_TIMEOUT: %w", err)
	}
	skip, err := strconv.ParseBool(envOr("UPSTREAM_INSECURE_SKIP_TLS_VERIFY",
		boolToStr(pickBool(fc.InsecureSkipTLSVerify, false))))
	if err != nil {
		return Config{}, fmt.Errorf("UPSTREAM_INSECURE_SKIP_TLS_VERIFY: %w", err)
	}
	return Config{
		Listen:                envOr("LISTEN", pickStr(fc.Listen, "127.0.0.1:18080")),
		UpstreamBase:          base,
		UpstreamWS:            ws,
		InsecureSkipTLSVerify: skip,
		ConnectTimeout:        ct,
		IdleTimeout:           it,
		LogLevel:              envOr("LOG_LEVEL", pickStr(fc.LogLevel, "info")),
	}, nil
}

func boolToStr(b bool) string {
	return strconv.FormatBool(b)
}

// configDir returns the directory holding config.json. It honors
// CC2WS_CONFIG_DIR (for tests); otherwise os.UserConfigDir()/cc2ws.
func configDir() (string, error) {
	if v := os.Getenv("CC2WS_CONFIG_DIR"); v != "" {
		return v, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "cc2ws"), nil
}

func ConfigPath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// fileConfig is the on-disk shape. Fields are pointers so absence is distinct
// from empty string: an absent field falls through to env/defaults rather than
// clobbering a non-empty env value with "".
type fileConfig struct {
	Listen                *string `json:"listen,omitempty"`
	UpstreamBase          *string `json:"upstream_base,omitempty"`
	InsecureSkipTLSVerify *bool   `json:"insecure_skip_tls_verify,omitempty"`
	ConnectTimeout        *string `json:"connect_timeout,omitempty"`
	IdleTimeout           *string `json:"idle_timeout,omitempty"`
	LogLevel              *string `json:"log_level,omitempty"`
}

// LoadFile reads config.json. Returns a zero fileConfig (no fields set) if the
// file is missing. A corrupt file logs a warning and returns zero — LoadConfig
// never fails to start because of a bad file.
func LoadFile() fileConfig {
	path, err := ConfigPath()
	if err != nil {
		return fileConfig{}
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return fileConfig{} // missing file is fine
	}
	var fc fileConfig
	if err := json.Unmarshal(b, &fc); err != nil {
		log.Printf("cc2ws: ignoring corrupt config file %s: %v", path, err)
		return fileConfig{}
	}
	return fc
}

// SaveConfig writes cfg to config.json, creating the directory.
func SaveConfig(cfg Config) error {
	dir, err := configDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, "config.json")
	ct := cfg.ConnectTimeout.String()
	it := cfg.IdleTimeout.String()
	fc := fileConfig{
		Listen:                &cfg.Listen,
		UpstreamBase:          &cfg.UpstreamBase,
		InsecureSkipTLSVerify: &cfg.InsecureSkipTLSVerify,
		ConnectTimeout:        &ct,
		IdleTimeout:           &it,
		LogLevel:              &cfg.LogLevel,
	}
	b, err := json.MarshalIndent(fc, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

// pickStr returns *s if non-nil, else def.
func pickStr(s *string, def string) string {
	if s != nil {
		return *s
	}
	return def
}

// pickBool returns *b if non-nil, else def.
func pickBool(b *bool, def bool) bool {
	if b != nil {
		return *b
	}
	return def
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
