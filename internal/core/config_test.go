package core

import "testing"

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
	t.Setenv("UPSTREAM_BASE", "")
	_, err := LoadConfig()
	if err == nil {
		t.Fatal("expected error when UPSTREAM_BASE unset")
	}
}

func TestLoadConfigOK(t *testing.T) {
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
