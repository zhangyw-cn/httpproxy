package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"httpproxy/internal/api"
	"httpproxy/internal/cert"
	"httpproxy/internal/config"
	"httpproxy/internal/flow"
	"httpproxy/internal/proxy"
	"httpproxy/internal/store"
)

func TestConfigHTTP(t *testing.T) {
	dir, file, err := config.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	session := config.NewSession(dir, file, config.Overrides{}, file.Proxy, file.UI, file.MaxFlows, file.MaxBody)
	_, runtime := session.Snapshot()
	st := store.New(runtime.MaxFlows)
	ca, err := cert.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	px := proxy.New("127.0.0.1:0", nil, runtime.MaxBody, ca)
	srv := api.New(st, ca, session, px)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	res := request(t, http.MethodGet, ts.URL+"/api/config")
	var snap struct {
		File    config.Values `json:"file"`
		Runtime config.Values `json:"runtime"`
	}
	decodeJSON(t, res, &snap)
	if snap.File.MaxFlows != 1000 || snap.Runtime.MaxFlows != 1000 {
		t.Fatalf("max_flows file=%d runtime=%d, want 1000", snap.File.MaxFlows, snap.Runtime.MaxFlows)
	}

	st.Add(flow.Flow{ID: "1"})
	st.Add(flow.Flow{ID: "2"})
	st.Add(flow.Flow{ID: "3"})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws/flows"
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	waitForClient(t, srv)

	res = putBody(t, ts.URL+"/api/config", `{"proxy":"127.0.0.1:9090","ui":"127.0.0.1:9091","max_flows":1,"max_body":8,"theme":"light","capture":true,"capture_empty":"all","capture_https":"decrypt","capture_hosts":[],"hosts_profile":""}`)
	decodeJSON(t, res, &snap)
	if snap.File.Proxy != "127.0.0.1:9090" {
		t.Fatalf("file.proxy = %q", snap.File.Proxy)
	}
	if snap.Runtime.Proxy != "127.0.0.1:8080" {
		t.Fatalf("runtime.proxy = %q", snap.Runtime.Proxy)
	}
	if snap.Runtime.Theme != "light" {
		t.Fatalf("runtime.theme = %q", snap.Runtime.Theme)
	}
	if st.Max() != 1 {
		t.Fatalf("store.Max() = %d, want 1", st.Max())
	}
	if got := reflect.ValueOf(px).Elem().FieldByName("maxBody").Int(); got != 8 {
		t.Fatalf("maxBody = %d, want 8", got)
	}

	var evicted []string
	for len(evicted) < 2 {
		var event struct {
			Type   string `json:"type"`
			FlowID string `json:"flow_id"`
		}
		if err := wsjsonRead(ctx, conn, &event); err != nil {
			t.Fatal(err)
		}
		if event.Type != "flow.evicted" {
			t.Fatalf("event type = %q", event.Type)
		}
		evicted = append(evicted, event.FlowID)
	}
	if evicted[0] != "1" || evicted[1] != "2" {
		t.Fatalf("evicted = %v", evicted)
	}
	if _, ok := st.Get("3"); !ok {
		t.Fatal("flow 3 was evicted")
	}
	if _, ok := st.Get("1"); ok {
		t.Fatal("flow 1 still exists")
	}

	saved, err := os.ReadFile(filepath.Join(dir.Path(), "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(saved, []byte("127.0.0.1:9090")) {
		t.Fatalf("config file was not saved: %s", saved)
	}

	res = putBody(t, ts.URL+"/api/config", `{"max_flows":1}`)
	assertConfigError(t, res, http.StatusBadRequest, "配置缺少字段")
	after, err := os.ReadFile(filepath.Join(dir.Path(), "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(saved, after) {
		t.Fatal("config file bytes changed")
	}

	res = putBody(t, ts.URL+"/api/config", `{"proxy":"nope","ui":"127.0.0.1:8081","max_flows":1,"max_body":1,"theme":"dark","capture":true,"capture_empty":"all","capture_https":"decrypt","capture_hosts":[],"hosts_profile":""}`)
	assertConfigError(t, res, http.StatusBadRequest, "代理地址必须是 host:port")

	res = putBody(t, ts.URL+"/api/config", `{`)
	assertConfigError(t, res, http.StatusBadRequest, "配置不是合法的 JSON")

	after, err = os.ReadFile(filepath.Join(dir.Path(), "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(saved, after) {
		t.Fatal("config file bytes changed")
	}
	if st.Max() != 1 {
		t.Fatalf("store.Max() = %d, want 1", st.Max())
	}
	if got := reflect.ValueOf(px).Elem().FieldByName("maxBody").Int(); got != 8 {
		t.Fatalf("maxBody = %d, want 8", got)
	}
}

func TestConfigMaxFlowsOverride(t *testing.T) {
	dir, file, err := config.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	session := config.NewSession(dir, file, config.Overrides{MaxFlows: true}, file.Proxy, file.UI, 50, file.MaxBody)
	_, runtime := session.Snapshot()
	if runtime.MaxFlows != 50 {
		t.Fatalf("runtime max_flows = %d, want 50", runtime.MaxFlows)
	}
	st := store.New(runtime.MaxFlows)
	ca, err := cert.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	srv := api.New(st, ca, session, nil)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	res := putBody(t, ts.URL+"/api/config", `{"proxy":"127.0.0.1:8080","ui":"127.0.0.1:8081","max_flows":2,"max_body":1048576,"theme":"dark","capture":true,"capture_empty":"all","capture_https":"decrypt","capture_hosts":[],"hosts_profile":""}`)
	var snap struct {
		File    config.Values `json:"file"`
		Runtime config.Values `json:"runtime"`
	}
	decodeJSON(t, res, &snap)
	if snap.File.MaxFlows != 2 {
		t.Fatalf("file.max_flows = %d, want 2", snap.File.MaxFlows)
	}
	if snap.Runtime.MaxFlows != 50 {
		t.Fatalf("runtime.max_flows = %d, want 50", snap.Runtime.MaxFlows)
	}
	if st.Max() != 50 {
		t.Fatalf("store.Max() = %d, want 50", st.Max())
	}
}

func TestConfigValidationMessages(t *testing.T) {
	dir, file, err := config.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	session := config.NewSession(dir, file, config.Overrides{}, file.Proxy, file.UI, file.MaxFlows, file.MaxBody)
	ca, err := cert.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	srv := api.New(store.New(file.MaxFlows), ca, session, nil)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	before, err := os.ReadFile(filepath.Join(dir.Path(), "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		body string
		msg  string
	}{
		{`{"proxy":"127.0.0.1:8080","ui":"nope","max_flows":1,"max_body":1,"theme":"dark","capture":true,"capture_empty":"all","capture_https":"decrypt","capture_hosts":[],"hosts_profile":""}`, "UI 地址必须是 host:port"},
		{`{"proxy":"127.0.0.1:8080","ui":"127.0.0.1:8081","max_flows":0,"max_body":1,"theme":"dark","capture":true,"capture_empty":"all","capture_https":"decrypt","capture_hosts":[],"hosts_profile":""}`, "保留条数必须是大于等于 1 的整数"},
		{`{"proxy":"127.0.0.1:8080","ui":"127.0.0.1:8081","max_flows":1,"max_body":-1,"theme":"dark","capture":true,"capture_empty":"all","capture_https":"decrypt","capture_hosts":[],"hosts_profile":""}`, "正文上限必须是大于等于 0 的整数"},
		{`{"proxy":"127.0.0.1:8080","ui":"127.0.0.1:8081","max_flows":1,"max_body":1,"theme":"blue","capture":true,"capture_empty":"all","capture_https":"decrypt","capture_hosts":[],"hosts_profile":""}`, "主题必须是 dark 或 light"},
		{`{"proxy":"127.0.0.1:8080","ui":"127.0.0.1:8081","max_flows":1,"max_body":1,"theme":"dark","extra":true,"capture":true,"capture_empty":"all","capture_https":"decrypt","capture_hosts":[],"hosts_profile":""}`, "配置含有未知字段"},
	}
	for _, tc := range cases {
		res := putBody(t, ts.URL+"/api/config", tc.body)
		assertConfigError(t, res, http.StatusBadRequest, tc.msg)
	}
	after, err := os.ReadFile(filepath.Join(dir.Path(), "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("config file bytes changed")
	}
}

func TestSetProxyAppliesLiveBodyLimit(t *testing.T) {
	dir, file, err := config.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	session := config.NewSession(dir, file, config.Overrides{}, file.Proxy, file.UI, file.MaxFlows, file.MaxBody)
	ca, err := cert.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	px := proxy.New("127.0.0.1:0", nil, file.MaxBody, ca)
	srv := api.New(store.New(file.MaxFlows), ca, session, nil)
	srv.SetProxy(px)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	res := putBody(t, ts.URL+"/api/config", `{"proxy":"127.0.0.1:8080","ui":"127.0.0.1:8081","max_flows":1,"max_body":8,"theme":"dark","capture":true,"capture_empty":"all","capture_https":"decrypt","capture_hosts":[],"hosts_profile":""}`)
	var snap struct {
		File    config.Values `json:"file"`
		Runtime config.Values `json:"runtime"`
	}
	decodeJSON(t, res, &snap)
	if got := reflect.ValueOf(px).Elem().FieldByName("maxBody").Int(); got != 8 {
		t.Fatalf("maxBody = %d, want 8", got)
	}
}

func TestConfigNilSession(t *testing.T) {
	ca, err := cert.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	srv := api.New(store.New(1), ca, nil, nil)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	res := request(t, http.MethodGet, ts.URL+"/api/config")
	assertConfigError(t, res, http.StatusInternalServerError, "无法写入配置")

	res = putBody(t, ts.URL+"/api/config", `{"proxy":"127.0.0.1:8080","ui":"127.0.0.1:8081","max_flows":1,"max_body":1,"theme":"dark","capture":true,"capture_empty":"all","capture_https":"decrypt","capture_hosts":[],"hosts_profile":""}`)
	assertConfigError(t, res, http.StatusInternalServerError, "无法写入配置")
}

func TestPutCapture(t *testing.T) {
	dir, file, err := config.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	session := config.NewSession(dir, file, config.Overrides{}, file.Proxy, file.UI, file.MaxFlows, file.MaxBody)
	st := store.New(file.MaxFlows)
	px := proxy.New("127.0.0.1:0", nil, file.MaxBody, nil)
	srv := api.New(st, nil, session, px)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	res := putBody(t, ts.URL+"/api/capture", `{"capture":false}`)
	var snap struct {
		File    config.Values `json:"file"`
		Runtime config.Values `json:"runtime"`
	}
	decodeJSON(t, res, &snap)
	if snap.File.Capture || snap.Runtime.Capture {
		t.Fatal("capture still on")
	}
	policy := reflect.ValueOf(px).Elem().FieldByName("policy")
	if policy.FieldByName("capture").Bool() {
		t.Fatal("proxy policy still captures")
	}
	if !policy.FieldByName("emptyAll").Bool() || policy.FieldByName("tunnel").Bool() {
		t.Fatal("proxy policy changed more than the capture switch")
	}
	if snap.File.Proxy != file.Proxy || snap.File.MaxFlows != file.MaxFlows {
		t.Fatalf("other fields changed: %+v", snap.File)
	}
	saved, err := os.ReadFile(filepath.Join(dir.Path(), "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(saved, []byte("capture: false")) {
		t.Fatalf("file: %s", saved)
	}

	res = putBody(t, ts.URL+"/api/capture", `{"capture":false,"theme":"light"}`)
	assertConfigError(t, res, http.StatusBadRequest, "配置含有未知字段")
	res = putBody(t, ts.URL+"/api/capture", `{}`)
	assertConfigError(t, res, http.StatusBadRequest, "配置缺少字段")
	res = putBody(t, ts.URL+"/api/capture", `{"capture":"false"}`)
	assertConfigError(t, res, http.StatusBadRequest, "抓包开关必须是布尔值")

	body := `{"proxy":"127.0.0.1:8080","ui":"127.0.0.1:8081","max_flows":1000,"max_body":1048576,"theme":"dark","capture":false,"capture_empty":"none","capture_https":"tunnel","capture_hosts":["*"],"hosts_profile":""}`
	res = putBody(t, ts.URL+"/api/config", body)
	assertConfigError(t, res, http.StatusBadRequest, "主机规则不合法")
	after, err := os.ReadFile(filepath.Join(dir.Path(), "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(saved, after) {
		t.Fatal("invalid save changed the file")
	}
}

func TestSavedAllowlistAppliesToTheNextRequest(t *testing.T) {
	dir, file, err := config.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	session := config.NewSession(dir, file, config.Overrides{}, file.Proxy, file.UI, file.MaxFlows, file.MaxBody)
	st := store.New(file.MaxFlows)
	ca, err := cert.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	srv := api.New(st, ca, session, nil)
	px := proxy.New("127.0.0.1:0", srv.Sink(), file.MaxBody, ca)
	srv.SetProxy(px)
	errCh := make(chan error, 1)
	go func() { errCh <- px.ListenAndServe() }()
	t.Cleanup(func() {
		_ = px.Close()
		if err := <-errCh; err != nil {
			t.Errorf("ListenAndServe: %v", err)
		}
	})
	var addr string
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if addr = px.Addr(); addr != "" {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if addr == "" {
		t.Fatal("proxy did not listen")
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "proxied")
	}))
	defer upstream.Close()
	put := func(empty, hosts string) {
		t.Helper()
		body := fmt.Sprintf(`{"proxy":%q,"ui":%q,"max_flows":%d,"max_body":%d,"theme":%q,"capture":true,"capture_empty":%q,"capture_https":"decrypt","capture_hosts":%s,"hosts_profile":""}`,
			file.Proxy, file.UI, file.MaxFlows, file.MaxBody, file.Theme, empty, hosts)
		res := putBody(t, ts.URL+"/api/config", body)
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			msg, _ := io.ReadAll(res.Body)
			t.Fatalf("status %d body %s", res.StatusCode, msg)
		}
	}
	proxyURL, err := url.Parse("http://" + addr)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}}
	get := func() {
		t.Helper()
		res, err := client.Get(upstream.URL)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		got, _ := io.ReadAll(res.Body)
		if res.StatusCode != http.StatusOK || string(got) != "proxied" {
			t.Fatalf("status %d body %q", res.StatusCode, got)
		}
	}
	put("none", `["example.com"]`)
	get()
	quiet := time.Now().Add(3 * time.Second)
	for time.Now().Before(quiet) {
		if flows := st.List(store.Filter{}); len(flows) != 0 {
			t.Fatalf("unmatched request recorded %d flows", len(flows))
		}
		time.Sleep(time.Millisecond)
	}
	put("all", `[]`)
	get()
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if flows := st.List(store.Filter{}); len(flows) == 1 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("allow-all save recorded %d flows", len(st.List(store.Filter{})))
}

func putBody(t *testing.T, url, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPut, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func assertConfigError(t *testing.T, res *http.Response, status int, msg string) {
	t.Helper()
	defer res.Body.Close()
	if res.StatusCode != status {
		t.Fatalf("status = %d, want %d", res.StatusCode, status)
	}
	var body struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Error != msg {
		t.Fatalf("error = %q, want %q", body.Error, msg)
	}
}
