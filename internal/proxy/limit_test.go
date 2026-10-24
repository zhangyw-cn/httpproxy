package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"httpproxy/internal/flow"
)

type discardSink struct{}

func (discardSink) OnCreated(flow.Flow) {}
func (discardSink) OnUpdated(flow.Flow) {}

func TestFlowKeepsStartLimit(t *testing.T) {
	p := New("127.0.0.1:0", discardSink{}, 4, nil)

	req := httptest.NewRequest(http.MethodPost, "http://example.com/", strings.NewReader("abcdefghij"))
	var first flow.Flow
	reqCapture, limit := p.startFlow(req, &first)
	if limit != 4 {
		t.Fatalf("start limit = %d, want 4", limit)
	}
	if _, err := io.ReadAll(req.Body); err != nil {
		t.Fatal(err)
	}
	stored, _, truncated := reqCapture.result()
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
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(respBody)),
	}
	p.finishResponse(&first, reqCapture, req, resp, respCapture, limit)
	if got := string(first.RespBody); got != "1234" {
		t.Fatalf("stored response prefix = %q, want 4 bytes %q", got, "1234")
	}
}
