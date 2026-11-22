package api_test

import (
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/zhangyw-cn/httpproxy/internal/config"
	"github.com/zhangyw-cn/httpproxy/internal/proxy"
)

func capturePolicy(t *testing.T, px *proxy.Proxy) reflect.Value {
	t.Helper()
	return reflect.ValueOf(px).Elem().FieldByName("policy")
}

func TestCaptureRules(t *testing.T) {
	_, ts, dir, _, px := newHostsServer(t)

	res := postBody(t, ts.URL+"/api/hosts", `{"name":"测试"}`)
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("create status = %d", res.StatusCode)
	}
	res.Body.Close()
	res = putBody(t, hostURL(ts, "测试"), `{"content":"10.0.0.5 api.test\n"}`)
	res.Body.Close()
	res = putBody(t, ts.URL+"/api/hosts", `{"active":"测试"}`)
	res.Body.Close()
	res = putBody(t, ts.URL+"/api/capture", `{"capture":true}`)
	res.Body.Close()

	res = putBody(t, ts.URL+"/api/capture/rules", `{"capture_empty":"none","capture_https":"tunnel","capture_hosts":[" example.com ","","  ","cdn.test"]}`)
	var snap struct {
		File    config.Values `json:"file"`
		Runtime config.Values `json:"runtime"`
	}
	decodeJSON(t, res, &snap)
	if snap.File.CaptureEmpty != "none" || snap.Runtime.CaptureEmpty != "none" {
		t.Fatalf("capture_empty file=%q runtime=%q", snap.File.CaptureEmpty, snap.Runtime.CaptureEmpty)
	}
	if snap.File.CaptureHTTPS != "tunnel" || snap.Runtime.CaptureHTTPS != "tunnel" {
		t.Fatalf("capture_https file=%q runtime=%q", snap.File.CaptureHTTPS, snap.Runtime.CaptureHTTPS)
	}
	wantHosts := []string{"example.com", "cdn.test"}
	if !reflect.DeepEqual(snap.File.CaptureHosts, wantHosts) || !reflect.DeepEqual(snap.Runtime.CaptureHosts, wantHosts) {
		t.Fatalf("capture_hosts file=%#v runtime=%#v, want %#v", snap.File.CaptureHosts, snap.Runtime.CaptureHosts, wantHosts)
	}
	if !snap.File.Capture || !snap.Runtime.Capture {
		t.Fatal("capture was not preserved")
	}
	if snap.File.HostsProfile != "测试" || snap.Runtime.HostsProfile != "测试" {
		t.Fatalf("hosts_profile file=%q runtime=%q", snap.File.HostsProfile, snap.Runtime.HostsProfile)
	}
	if snap.File.Proxy != config.Defaults().Proxy {
		t.Fatalf("proxy = %q", snap.File.Proxy)
	}

	saved, err := os.ReadFile(filepath.Join(dir.Path(), "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`capture: true`,
		`capture_empty: "none"`,
		`capture_https: "tunnel"`,
		`hosts_profile: "测试"`,
		`- "example.com"`,
		`- "cdn.test"`,
	} {
		if !bytes.Contains(saved, []byte(want)) {
			t.Fatalf("config.yaml missing %q:\n%s", want, saved)
		}
	}

	policy := capturePolicy(t, px)
	if !policy.FieldByName("capture").Bool() {
		t.Fatal("proxy policy no longer captures")
	}
	if policy.FieldByName("emptyAll").Bool() || !policy.FieldByName("tunnel").Bool() {
		t.Fatal("proxy policy did not pick up the new rules")
	}
	if got := policy.FieldByName("rules").Len(); got != len(wantHosts) {
		t.Fatalf("proxy policy rules = %d, want %d", got, len(wantHosts))
	}

	assertConfigError(t, putBody(t, ts.URL+"/api/capture/rules", `{`), http.StatusBadRequest, "配置不是合法的 JSON")
	assertConfigError(t, putBody(t, ts.URL+"/api/capture/rules", `{"capture_empty":"none","capture_https":"tunnel"}`), http.StatusBadRequest, "配置缺少字段")
	assertConfigError(t, putBody(t, ts.URL+"/api/capture/rules", `{"capture_empty":"none","capture_https":"tunnel","capture_hosts":[],"capture":true}`), http.StatusBadRequest, "配置含有未知字段")
	assertConfigError(t, putBody(t, ts.URL+"/api/capture/rules", `{"capture_empty":1,"capture_https":"tunnel","capture_hosts":[]}`), http.StatusBadRequest, "空规则必须是 all 或 none")
	assertConfigError(t, putBody(t, ts.URL+"/api/capture/rules", `{"capture_empty":"none","capture_https":true,"capture_hosts":[]}`), http.StatusBadRequest, "未抓 HTTPS 必须是 decrypt 或 tunnel")
	assertConfigError(t, putBody(t, ts.URL+"/api/capture/rules", `{"capture_empty":"none","capture_https":"tunnel","capture_hosts":"example.com"}`), http.StatusBadRequest, "主机规则必须是字符串列表")
	assertConfigError(t, putBody(t, ts.URL+"/api/capture/rules", `{"capture_empty":"none","capture_https":"tunnel","capture_hosts":null}`), http.StatusBadRequest, "配置缺少字段")
	assertConfigError(t, putBody(t, ts.URL+"/api/capture/rules", `{"capture_empty":"none","capture_https":"tunnel","capture_hosts":["*"]}`), http.StatusBadRequest, "主机规则不合法")
	assertConfigError(t, putBody(t, ts.URL+"/api/capture/rules", `{"capture_empty":"none","capture_https":"tunnel","capture_hosts":["a.test"," a.test "]}`), http.StatusBadRequest, "主机规则重复")

	after, err := os.ReadFile(filepath.Join(dir.Path(), "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(saved, after) {
		t.Fatal("an invalid rules save changed config.yaml")
	}
}
