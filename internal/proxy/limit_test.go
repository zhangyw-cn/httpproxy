package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"httpproxy/internal/flow"
)

func TestFlowKeepsStartLimit(t *testing.T) {
	sink := &memSink{}
	p := New("127.0.0.1:0", sink, 4, nil)
	t.Cleanup(func() { _ = p.Close() })

	req := httptest.NewRequest(http.MethodPost, "http://example.com/", strings.NewReader("abcdefghij"))
	var first flow.Flow
	rec, limit := p.startFlow(req, &first)
	if limit != 4 {
		t.Fatalf("start limit = %d, want 4", limit)
	}
	if _, err := io.ReadAll(req.Body); err != nil {
		t.Fatal(err)
	}
	stored, _, truncated := rec.reqCap.result()
	if string(stored) != "abcd" || !truncated {
		t.Fatalf("request capture = %q truncated=%t, want %q truncated", stored, truncated, "abcd")
	}

	p.SetMaxBody(2)
	req2 := httptest.NewRequest(http.MethodPost, "http://example.com/", strings.NewReader("abcdefghij"))
	var second flow.Flow
	_, limit2 := p.startFlow(req2, &second)
	if limit2 != 2 {
		t.Fatalf("second start limit = %d, want 2", limit2)
	}

	const respBody = "12345678"
	respCapture := newBodyCapture(limit)
	if _, err := io.Copy(respCapture, strings.NewReader(respBody)); err != nil {
		t.Fatal(err)
	}
	resp := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header)}
	p.finishRecord(rec, resp.Header, respCapture)
	got := waitUpdatedFlow(t, sink)
	if string(got.RespBody) != "1234" {
		t.Fatalf("stored response prefix = %q, want 4 bytes %q", got.RespBody, "1234")
	}
}
