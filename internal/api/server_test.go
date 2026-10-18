package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"httpproxy/internal/api"
	"httpproxy/internal/cert"
	"httpproxy/internal/flow"
	"httpproxy/internal/store"
)

func TestListGetClearAndCA(t *testing.T) {
	st := store.New(10)
	st.Add(flow.Flow{
		ID: "1", Host: "api.example.com", Path: "/users", StatusCode: 200,
		ReqBody: []byte("request body"),
	})
	st.Add(flow.Flow{ID: "2", Host: "cdn.example.com", Path: "/asset", StatusCode: 404})
	ca, err := cert.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	srv := api.New(st, ca)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	res := request(t, http.MethodGet, ts.URL+"/api/health")
	var health struct {
		OK bool `json:"ok"`
	}
	decodeJSON(t, res, &health)
	if !health.OK {
		t.Fatal("health response should be ok")
	}

	res = request(t, http.MethodGet, ts.URL+"/api/flows?host=api&path=users&status=2&limit=1")
	var summaries []map[string]any
	decodeJSON(t, res, &summaries)
	if len(summaries) != 1 || summaries[0]["id"] != "1" {
		t.Fatalf("unexpected summaries: %#v", summaries)
	}
	if _, ok := summaries[0]["req_body"]; ok {
		t.Fatal("flow summary must not include req_body")
	}

	res = request(t, http.MethodGet, ts.URL+"/api/flows/1")
	var full flow.Flow
	decodeJSON(t, res, &full)
	if !bytes.Equal(full.ReqBody, []byte("request body")) {
		t.Fatalf("unexpected request body: %q", full.ReqBody)
	}

	res = request(t, http.MethodGet, ts.URL+"/api/flows/missing")
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("missing flow status = %d, want 404", res.StatusCode)
	}
	res.Body.Close()

	res = request(t, http.MethodDelete, ts.URL+"/api/flows")
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("clear status = %d, want 204", res.StatusCode)
	}
	res.Body.Close()

	res = request(t, http.MethodGet, ts.URL+"/api/flows")
	var empty []flow.FlowSummary
	decodeJSON(t, res, &empty)
	if empty == nil || len(empty) != 0 {
		t.Fatalf("want empty JSON array, got %#v", empty)
	}

	res = request(t, http.MethodGet, ts.URL+"/api/ca.crt")
	if got := res.Header.Get("Content-Type"); got != "application/x-x509-ca-cert" {
		t.Fatalf("content type = %q", got)
	}
	pem, err := io.ReadAll(res.Body)
	res.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(pem, []byte("BEGIN CERTIFICATE")) {
		t.Fatal("CA response is not PEM")
	}
}

func TestWebSocketReceivesSinkEvents(t *testing.T) {
	st := store.New(10)
	ca, err := cert.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	srv := api.New(st, ca)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws/flows"
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()

	srv.Sink().OnCreated(flow.Flow{ID: "created", Host: "example.com"})

	var event struct {
		Type string            `json:"type"`
		Flow *flow.FlowSummary `json:"flow"`
	}
	if err := wsjsonRead(ctx, conn, &event); err != nil {
		t.Fatal(err)
	}
	if event.Type != "flow.created" || event.Flow == nil || event.Flow.ID != "created" {
		t.Fatalf("unexpected event: %+v", event)
	}
	if _, ok := st.Get("created"); !ok {
		t.Fatal("sink did not add flow to store")
	}
}

func request(t *testing.T, method, url string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func decodeJSON(t *testing.T, res *http.Response, dst any) {
	t.Helper()
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		t.Fatalf("unexpected HTTP status: %s", res.Status)
	}
	if err := json.NewDecoder(res.Body).Decode(dst); err != nil {
		t.Fatal(err)
	}
}

func wsjsonRead(ctx context.Context, conn *websocket.Conn, dst any) error {
	_, data, err := conn.Read(ctx)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, dst)
}
