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
	Language              string // "zh" (default) | "en"
	AutoStart             bool
	ThemeMode             string // "dark" (default) | "light"
}

// normalizeUpstream parses an upstream origin given as http(s):// or ws(s)://
// and returns (base, ws): base is always an http/https origin (what we store
// and display), ws is the ws/wss URL the proxy dials. Path/query/fragment are
// stripped from both — the proxy appends the request path at dial time.
func normalizeUpstream(in string) (base, ws string, err error) {
	u, err := url.Parse(in)
	if err != nil {
		return "", "", fmt.Errorf("parse upstream base: %w", err)
	}
	switch u.Scheme {
	case "https", "wss":
		u.Scheme = "wss"
	case "http", "ws":
		u.Scheme = "ws"
	default:
		return "", "", fmt.Errorf("upstream base must be http(s):// or ws(s)://, got scheme %q", u.Scheme)
	}
	if u.Host == "" {
		return "", "", fmt.Errorf("upstream base missing host")
	}
	u.Path = ""
	u.RawPath = ""
	u.RawQuery = ""
	u.Fragment = ""
	ws = u.String()
	switch u.Scheme {
	case "wss":
		u.Scheme = "https"
	case "ws":
		u.Scheme = "http"
	}
	return u.String(), ws, nil
}

// swapScheme returns just the WebSocket URL for base. Kept as a thin wrapper
// over normalizeUpstream so existing callers and tests compile unchanged.
func swapScheme(base string) (string, error) {
	_, ws, err := normalizeUpstream(base)
	return ws, err
}

// DefaultConfig returns a usable Config with an empty upstream and sensible
// defaults. Used on the GUI path when LoadConfig fails (e.g. a fresh install
// with no UPSTREAM_BASE) so the window still opens and the user can fill in
// the upstream on the Settings tab instead of the app exiting silently.
func DefaultConfig() Config {
	return Config{
		Listen:         "127.0.0.1:18080",
		UpstreamBase:   "",
		UpstreamWS:     "",
		ConnectTimeout: 10 * time.Second,
		IdleTimeout:    600 * time.Second,
		LogLevel:       "info",
		Language:       "zh",
		ThemeMode:      "dark",
	}
}

func LoadConfig() (Config, error) {
	fc := LoadFile()

	// env wins over file; file wins over defaults.
	base := envOr("UPSTREAM_BASE", pickStr(fc.UpstreamBase, ""))
	if base == "" {
		return Config{}, fmt.Errorf("UPSTREAM_BASE is required")
	}
	nb, ws, err := normalizeUpstream(base)
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
	lang := envOr("CC2WS_LANG", pickStr(fc.Language, "zh"))
	autoStart, err := strconv.ParseBool(envOr("CC2WS_AUTOSTART",
		boolToStr(pickBool(fc.AutoStart, false))))
	if err != nil {
		return Config{}, fmt.Errorf("CC2WS_AUTOSTART: %w", err)
	}
	themeMode := envOr("CC2WS_THEME", pickStr(fc.ThemeMode, "dark"))
	return Config{
		Listen:                envOr("LISTEN", pickStr(fc.Listen, "127.0.0.1:18080")),
		UpstreamBase:          nb,
		UpstreamWS:            ws,
		InsecureSkipTLSVerify: skip,
		ConnectTimeout:        ct,
		IdleTimeout:           it,
		LogLevel:              envOr("LOG_LEVEL", pickStr(fc.LogLevel, "info")),
		Language:              lang,
		AutoStart:             autoStart,
		ThemeMode:             themeMode,
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
	Language              *string `json:"language,omitempty"`
	AutoStart             *bool   `json:"autostart,omitempty"`
	ThemeMode             *string `json:"theme_mode,omitempty"`
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
		Language:              &cfg.Language,
		AutoStart:             &cfg.AutoStart,
		ThemeMode:             &cfg.ThemeMode,
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

// Validate returns nil if cfg's upstream origin and durations parse cleanly
// and the log level is one of debug/info/warn/error. Used by the GUI "Save &
// Apply" before calling SetConfig.
func Validate(cfg Config) error {
	if _, err := swapScheme(cfg.UpstreamBase); err != nil {
		return err
	}
	if cfg.Language == "" {
		cfg.Language = "zh"
	}
	switch cfg.Language {
	case "zh", "en":
	default:
		return fmt.Errorf("invalid language %q (want zh or en)", cfg.Language)
	}
	if cfg.ThemeMode == "" {
		cfg.ThemeMode = "dark"
	}
	switch cfg.ThemeMode {
	case "dark", "light":
	default:
		return fmt.Errorf("invalid theme_mode %q (want dark or light)", cfg.ThemeMode)
	}
	if cfg.ConnectTimeout <= 0 {
		return fmt.Errorf("connect timeout must be > 0")
	}
	if cfg.IdleTimeout <= 0 {
		return fmt.Errorf("idle timeout must be > 0")
	}
	switch cfg.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("invalid log level %q", cfg.LogLevel)
	}
	return nil
}

// BuildConfigFromStrings parses the GUI form's string fields into a validated
// Config (computing UpstreamWS). Pure — no Fyne — so it's testable on every
// platform without a C compiler.
func BuildConfigFromStrings(upstream, listen, ct, it, level, language, themeMode string, skipTLS, autoStart bool) (Config, error) {
	ctd, err := time.ParseDuration(ct)
	if err != nil {
		return Config{}, fmt.Errorf("connect timeout: %w", err)
	}
	itd, err := time.ParseDuration(it)
	if err != nil {
		return Config{}, fmt.Errorf("idle timeout: %w", err)
	}
	nb, ws, err := normalizeUpstream(upstream)
	if err != nil {
		return Config{}, err
	}
	cfg := Config{
		Listen:                listen,
		UpstreamBase:          nb,
		UpstreamWS:            ws,
		InsecureSkipTLSVerify: skipTLS,
		ConnectTimeout:        ctd,
		IdleTimeout:           itd,
		LogLevel:              level,
		Language:              language,
		AutoStart:             autoStart,
		ThemeMode:             themeMode,
	}
	if err := Validate(cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}
