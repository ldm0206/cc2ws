package core

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSwapScheme(t *testing.T) {
	cases := []struct{ in, want string }{
		{"https://hubcn.jikuixie.me", "wss://hubcn.jikuixie.me"},
		{"http://127.0.0.1:8090", "ws://127.0.0.1:8090"},
		{"https://example.com/some/path?q=1", "wss://example.com"},
		{"https://host:8443", "wss://host:8443"},
	}
	for _, c := range cases {
		got, err := swapScheme(c.in)
		if err != nil {
			t.Fatalf("swapScheme(%q) error: %v", c.in, err)
		}
		if got != c.want {
			t.Errorf("swapScheme(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSwapSchemeInvalid(t *testing.T) {
	for _, bad := range []string{"ftp://x", "notaurl", ":/x"} {
		if _, err := swapScheme(bad); err == nil {
			t.Errorf("swapScheme(%q) expected error", bad)
		}
	}
}

func TestLoadConfigRequiresUpstream(t *testing.T) {
	t.Setenv("CC2WS_CONFIG_DIR", t.TempDir())
	t.Setenv("UPSTREAM_BASE", "")
	_, err := LoadConfig()
	if err == nil {
		t.Fatal("expected error when UPSTREAM_BASE unset")
	}
}

func TestLoadConfigOK(t *testing.T) {
	t.Setenv("CC2WS_CONFIG_DIR", t.TempDir())
	t.Setenv("UPSTREAM_BASE", "https://hub.example.com")
	t.Setenv("LISTEN", "127.0.0.1:19090")
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UpstreamWS != "wss://hub.example.com" {
		t.Errorf("UpstreamWS=%q", cfg.UpstreamWS)
	}
	if cfg.Listen != "127.0.0.1:19090" {
		t.Errorf("Listen=%q", cfg.Listen)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CC2WS_CONFIG_DIR", dir) // override for tests
	in := Config{
		Listen:                "127.0.0.1:20000",
		UpstreamBase:          "https://hub.example.com",
		InsecureSkipTLSVerify: true,
		ConnectTimeout:        15 * time.Second,
		IdleTimeout:           300 * time.Second,
		LogLevel:              "debug",
	}
	if err := SaveConfig(in); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	// File must be the lowest-precedence input above defaults, so LoadConfig
	// (which also reads env) should reflect the file when env is unset.
	t.Setenv("UPSTREAM_BASE", in.UpstreamBase) // file sets base too; either way
	out, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if out.Listen != in.Listen {
		t.Errorf("Listen=%q want %q", out.Listen, in.Listen)
	}
	if out.IdleTimeout != in.IdleTimeout {
		t.Errorf("IdleTimeout=%v want %v", out.IdleTimeout, in.IdleTimeout)
	}
	if !out.InsecureSkipTLSVerify {
		t.Errorf("InsecureSkipTLSVerify=false want true")
	}
}

func TestEnvOverridesFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CC2WS_CONFIG_DIR", dir)
	file := Config{
		Listen:       "127.0.0.1:20000",
		UpstreamBase: "https://from-file.example.com",
	}
	if err := SaveConfig(file); err != nil {
		t.Fatal(err)
	}
	// Env wins over file.
	t.Setenv("LISTEN", "127.0.0.1:30000")
	t.Setenv("UPSTREAM_BASE", "https://from-env.example.com")
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != "127.0.0.1:30000" {
		t.Errorf("Listen=%q want env value 127.0.0.1:30000", cfg.Listen)
	}
	if cfg.UpstreamBase != "https://from-env.example.com" {
		t.Errorf("UpstreamBase=%q want env value", cfg.UpstreamBase)
	}
}

func TestCorruptConfigFileIgnored(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CC2WS_CONFIG_DIR", dir)
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte("{not valid json"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Must NOT error — fall back to defaults+env.
	t.Setenv("UPSTREAM_BASE", "https://hub.example.com")
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig should ignore corrupt file, got: %v", err)
	}
	if cfg.UpstreamBase != "https://hub.example.com" {
		t.Errorf("UpstreamBase=%q", cfg.UpstreamBase)
	}
}

func TestConfigPath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CC2WS_CONFIG_DIR", dir)
	p, err := ConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(p) != "config.json" {
		t.Errorf("ConfigPath base=%q want config.json", filepath.Base(p))
	}
}

func TestValidate(t *testing.T) {
	good := Config{UpstreamBase: "https://hub.example.com", ConnectTimeout: 10 * time.Second, IdleTimeout: 600 * time.Second, LogLevel: "info"}
	if err := Validate(good); err != nil {
		t.Fatalf("good cfg: %v", err)
	}
	badScheme := good
	badScheme.UpstreamBase = "ftp://x"
	if err := Validate(badScheme); err == nil {
		t.Error("bad scheme should fail")
	}
	zeroCT := good
	zeroCT.ConnectTimeout = 0
	if err := Validate(zeroCT); err == nil {
		t.Error("zero connect timeout should fail")
	}
	badLevel := good
	badLevel.LogLevel = "verbose"
	if err := Validate(badLevel); err == nil {
		t.Error("bad level should fail")
	}
}

func TestBuildConfigFromStrings(t *testing.T) {
	cases := []struct {
		name              string
		upstream, listen  string
		ct, it, level     string
		skipTLS           bool
		wantErr           bool
	}{
		{"valid", "https://hub.example.com", "127.0.0.1:18080", "10s", "600s", "info", false, false},
		{"bad scheme", "ftp://x", "127.0.0.1:18080", "10s", "600s", "info", false, true},
		{"bad duration", "https://hub.example.com", "127.0.0.1:18080", "notaduration", "600s", "info", false, true},
		{"bad level", "https://hub.example.com", "127.0.0.1:18080", "10s", "600s", "verbose", false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg, err := BuildConfigFromStrings(c.upstream, c.listen, c.ct, c.it, c.level, c.skipTLS)
			if (err != nil) != c.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, c.wantErr)
			}
			if err == nil && cfg.UpstreamWS == "" {
				t.Error("UpstreamWS should be set on success")
			}
		})
	}
}
