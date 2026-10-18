package api_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"httpproxy/internal/api"
)

func TestLocalOnlyAllowsLoopback(t *testing.T) {
	h := api.LocalOnly(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8081/api/health", nil)
	req.Host = "127.0.0.1:8081"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestLocalOnlyRejectsForeignHost(t *testing.T) {
	h := api.LocalOnly(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodGet, "http://evil.example/api/flows", nil)
	req.Host = "evil.example"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status %d, want 403", rec.Code)
	}
}
