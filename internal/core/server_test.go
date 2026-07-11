package core

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func routerCfg() Config {
	return Config{UpstreamWS: "ws://127.0.0.1:1", ConnectTimeout: time.Second, IdleTimeout: time.Second}
}

func TestHealthEndpoint(t *testing.T) {
	r := newRouter(routerCfg())
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/health", nil))
	if rec.Code != 200 {
		t.Fatalf("status=%d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"status":"ok"`) {
		t.Errorf("body=%q", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"version"`) {
		t.Errorf("body missing version: %q", rec.Body.String())
	}
}

func TestUnknownPathReturns404(t *testing.T) {
	r := newRouter(routerCfg())
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/nope", nil))
	if rec.Code != 404 {
		t.Errorf("status=%d want 404", rec.Code)
	}
}

func TestKnownRoutesRegistered(t *testing.T) {
	r := newRouter(routerCfg())
	// Wrong method on a known POST path → 405 from Go 1.22 method routing.
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/v1/messages", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET /v1/messages status=%d want 405", rec.Code)
	}
}
