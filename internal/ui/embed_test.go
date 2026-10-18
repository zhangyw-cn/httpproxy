package ui_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"httpproxy/internal/ui"
)

func TestHandlerServesEmbeddedIndex(t *testing.T) {
	for _, path := range []string{"/", "/flows/123"} {
		t.Run(path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, path, nil)
			response := httptest.NewRecorder()

			ui.Handler().ServeHTTP(response, request)

			if response.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
			}
			body, err := io.ReadAll(response.Result().Body)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(body), "<title>httpproxy</title>") {
				t.Fatalf("response does not contain placeholder page: %q", body)
			}
		})
	}
}
