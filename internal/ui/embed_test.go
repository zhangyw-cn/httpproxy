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
			body := serve(t, "light", path)
			for _, want := range []string{
				"<title>httpproxy</title>",
				`lang="zh-CN"`,
				`data-theme="light"`,
			} {
				if !strings.Contains(body, want) {
					t.Fatalf("GET %s missing %q", path, want)
				}
			}
			if strings.Contains(body, "httpproxy.theme") {
				t.Fatalf("GET %s still contains httpproxy.theme", path)
			}
		})
	}

	dark := serve(t, "nope", "/")
	if !strings.Contains(dark, `data-theme="dark"`) {
		t.Fatal("invalid theme did not serve data-theme=dark")
	}

	index := serve(t, "light", "/")
	script := serve(t, "light", moduleSrc(t, index))
	if !strings.Contains(script, "下载 CA") {
		t.Fatal("embedded script missing 下载 CA")
	}
}

func serve(t *testing.T, theme, path string) string {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	response := httptest.NewRecorder()
	ui.Handler(theme).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("GET %s status = %d, want %d", path, response.Code, http.StatusOK)
	}
	body, err := io.ReadAll(response.Result().Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func moduleSrc(t *testing.T, html string) string {
	t.Helper()
	const open = `<script type="module"`
	i := strings.Index(html, open)
	if i < 0 {
		t.Fatal("index has no module script")
	}
	rest := html[i:]
	const marker = `src="`
	j := strings.Index(rest, marker)
	if j < 0 {
		t.Fatal("module script has no src")
	}
	rest = rest[j+len(marker):]
	k := strings.Index(rest, `"`)
	if k < 0 {
		t.Fatal("module script src is unterminated")
	}
	return rest[:k]
}
