package api_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"httpproxy/internal/api"
	"httpproxy/internal/cert"
	"httpproxy/internal/config"
	"httpproxy/internal/hosts"
	"httpproxy/internal/proxy"
	"httpproxy/internal/store"
)

type hostProfileResponse struct {
	Name     string          `json:"name"`
	Rules    int             `json:"rules"`
	Warnings []hosts.Warning `json:"warnings"`
	Error    string          `json:"error"`
}

type hostsListResponse struct {
	Active      string                `json:"active"`
	ActiveError string                `json:"active_error"`
	Profiles    []hostProfileResponse `json:"profiles"`
}

type hostDetailResponse struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

type hostSaveResponse struct {
	Name     string          `json:"name"`
	Rules    int             `json:"rules"`
	Warnings []hosts.Warning `json:"warnings"`
}

type hostErrorResponse struct {
	Error  string `json:"error"`
	Line   int    `json:"line"`
	Detail string `json:"detail"`
}

func newHostsServer(t *testing.T) (*api.Server, *httptest.Server, *config.Dir, *hosts.Profiles, *proxy.Proxy) {
	t.Helper()
	dir, file, err := config.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	session := config.NewSession(dir, file, config.Overrides{}, file.Proxy, file.UI, file.MaxFlows, file.MaxBody)
	profiles, err := hosts.OpenProfiles(dir.Path())
	if err != nil {
		t.Fatal(err)
	}
	ca, err := cert.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	px := proxy.New("127.0.0.1:0", nil, file.MaxBody, ca)
	srv := api.New(store.New(file.MaxFlows), ca, session, px)
	srv.SetHostsProfiles(profiles)
	if err := srv.InitHosts(); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return srv, ts, dir, profiles, px
}

func postBody(t *testing.T, url, body string) *http.Response {
	t.Helper()
	return postContentType(t, url, "application/json", body)
}

func postContentType(t *testing.T, url, contentType, body string) *http.Response {
	t.Helper()
	res, err := http.Post(url, contentType, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func hostURL(ts *httptest.Server, name string) string {
	return ts.URL + "/api/hosts/" + url.PathEscape(name)
}

func findProfile(t *testing.T, list hostsListResponse, name string) hostProfileResponse {
	t.Helper()
	for _, profile := range list.Profiles {
		if profile.Name == name {
			return profile
		}
	}
	t.Fatalf("profile %q not in %#v", name, list.Profiles)
	return hostProfileResponse{}
}

func TestHostsCRUD(t *testing.T) {
	_, ts, _, _, _ := newHostsServer(t)

	res := postBody(t, ts.URL+"/api/hosts", `{"name":"测试 环境"}`)
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("create status = %d, want 204", res.StatusCode)
	}
	res.Body.Close()

	res = request(t, http.MethodGet, ts.URL+"/api/hosts")
	var list hostsListResponse
	decodeJSON(t, res, &list)
	profile := findProfile(t, list, "测试 环境")
	if profile.Rules != 0 {
		t.Fatalf("created rules = %d, want 0", profile.Rules)
	}
	if profile.Warnings == nil {
		t.Fatal("warnings must be [] not null")
	}

	content := "10.0.0.5 api.test\n10.0.0.5 api.test\n"
	res = putBody(t, hostURL(ts, "测试 环境"), `{"content":`+jsonString(content)+`}`)
	var saved hostSaveResponse
	decodeJSON(t, res, &saved)
	if saved.Name != "测试 环境" || saved.Rules != 1 {
		t.Fatalf("saved = %+v", saved)
	}
	if len(saved.Warnings) != 1 {
		t.Fatalf("warnings = %#v, want one duplicate warning", saved.Warnings)
	}

	res = request(t, http.MethodGet, hostURL(ts, "测试 环境"))
	var detail hostDetailResponse
	decodeJSON(t, res, &detail)
	if detail.Name != "测试 环境" || detail.Content != content {
		t.Fatalf("detail = %+v", detail)
	}

	res = postBody(t, hostURL(ts, "测试 环境")+"/rename", `{"name":"预发"}`)
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("rename status = %d, want 204", res.StatusCode)
	}
	res.Body.Close()

	res = request(t, http.MethodGet, hostURL(ts, "预发"))
	decodeJSON(t, res, &detail)
	if detail.Content != content {
		t.Fatalf("renamed content = %q", detail.Content)
	}

	res = request(t, http.MethodDelete, hostURL(ts, "预发"))
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("delete status = %d, want 204", res.StatusCode)
	}
	res.Body.Close()

	res = request(t, http.MethodGet, hostURL(ts, "预发"))
	assertConfigError(t, res, http.StatusNotFound, "配置不存在")
}

func TestHostsValidationErrors(t *testing.T) {
	_, ts, _, profiles, _ := newHostsServer(t)

	res := postBody(t, ts.URL+"/api/hosts", `{"name":"bad/name"}`)
	assertConfigError(t, res, http.StatusBadRequest, "配置名不合法")

	res = postBody(t, ts.URL+"/api/hosts", `{"name":"重复"}`)
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("create status = %d, want 204", res.StatusCode)
	}
	res.Body.Close()
	res = postBody(t, ts.URL+"/api/hosts", `{"name":"重复"}`)
	assertConfigError(t, res, http.StatusConflict, "同名配置已存在")

	res = putBody(t, hostURL(ts, "重复"), `{"content":"not-an-ip api.test\n"}`)
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid content status = %d, want 400", res.StatusCode)
	}
	var parseErr hostErrorResponse
	if err := json.NewDecoder(res.Body).Decode(&parseErr); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(parseErr.Error, "第 1 行：") {
		t.Fatalf("error = %q, want line prefix", parseErr.Error)
	}
	if parseErr.Line != 1 {
		t.Fatalf("line = %d, want 1", parseErr.Line)
	}
	data, err := profiles.Read("重复")
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 0 {
		t.Fatalf("invalid content was written: %q", data)
	}

	res = putBody(t, ts.URL+"/api/hosts", `{"active":"重复"}`)
	var list hostsListResponse
	decodeJSON(t, res, &list)
	if list.Active != "重复" {
		t.Fatalf("active = %q, want 重复", list.Active)
	}

	res = request(t, http.MethodDelete, hostURL(ts, "重复"))
	assertConfigError(t, res, http.StatusConflict, "当前启用的 Hosts 配置不能删除")

	res = putBody(t, ts.URL+"/api/hosts", `{"active":"missing"}`)
	assertConfigError(t, res, http.StatusBadRequest, "配置不存在")

	// Activating a profile whose file is invalid reports the documented
	// activation error plus the line detail.
	if err := os.WriteFile(filepath.Join(profiles.Dir(), "坏文件.hosts"), []byte("bad\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res = putBody(t, ts.URL+"/api/hosts", `{"active":"坏文件"}`)
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid active status = %d, want 400", res.StatusCode)
	}
	var activeErr hostErrorResponse
	if err := json.NewDecoder(res.Body).Decode(&activeErr); err != nil {
		t.Fatal(err)
	}
	if activeErr.Error != "当前启用的 Hosts 配置无效" || activeErr.Line != 1 || !strings.HasPrefix(activeErr.Detail, "第 1 行：") {
		t.Fatalf("active error = %+v, want documented activation error", activeErr)
	}
}

func TestActivateHostsWritesConfig(t *testing.T) {
	_, ts, dir, _, _ := newHostsServer(t)

	res := postBody(t, ts.URL+"/api/hosts", `{"name":"测试"}`)
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("create status = %d, want 204", res.StatusCode)
	}
	res.Body.Close()
	res = putBody(t, hostURL(ts, "测试"), `{"content":"10.0.0.5 api.test\n"}`)
	res.Body.Close()

	res = putBody(t, ts.URL+"/api/hosts", `{"active":"测试"}`)
	var list hostsListResponse
	decodeJSON(t, res, &list)
	if list.Active != "测试" {
		t.Fatalf("active = %q, want 测试", list.Active)
	}
	if profile := findProfile(t, list, "测试"); profile.Rules != 1 {
		t.Fatalf("rules = %d, want 1", profile.Rules)
	}

	saved, err := os.ReadFile(filepath.Join(dir.Path(), "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(saved, []byte(`hosts_profile: "测试"`)) {
		t.Fatalf("config.yaml = %s", saved)
	}

	res = request(t, http.MethodGet, ts.URL+"/api/hosts")
	decodeJSON(t, res, &list)
	if list.Active != "测试" || list.ActiveError != "" {
		t.Fatalf("list = %+v", list)
	}

	res = putBody(t, ts.URL+"/api/hosts", `{"active":""}`)
	decodeJSON(t, res, &list)
	if list.Active != "" {
		t.Fatalf("active = %q after clear", list.Active)
	}
	saved, err = os.ReadFile(filepath.Join(dir.Path(), "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(saved, []byte(`hosts_profile: ""`)) {
		t.Fatalf("config.yaml after clear = %s", saved)
	}
}

func TestPutConfigRejectsBadHostsProfile(t *testing.T) {
	_, ts, dir, profiles, _ := newHostsServer(t)
	cfgPath := filepath.Join(dir.Path(), "config.yaml")
	before, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}

	res := putBody(t, ts.URL+"/api/config", `{"proxy":"127.0.0.1:8080","ui":"127.0.0.1:8081","max_flows":1000,"max_body":1048576,"theme":"dark","capture":false,"capture_empty":"none","capture_https":"tunnel","capture_hosts":[],"hosts_profile":"missing"}`)
	assertConfigError(t, res, http.StatusBadRequest, "配置不存在")

	after, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("config file changed after rejected hosts_profile")
	}

	res = postBody(t, ts.URL+"/api/hosts", `{"name":"测试"}`)
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("create status = %d", res.StatusCode)
	}
	res.Body.Close()
	res = putBody(t, hostURL(ts, "测试"), `{"content":"10.0.0.5 api.test\n"}`)
	res.Body.Close()

	res = putBody(t, ts.URL+"/api/config", `{"proxy":"127.0.0.1:8080","ui":"127.0.0.1:8081","max_flows":1000,"max_body":1048576,"theme":"dark","capture":false,"capture_empty":"none","capture_https":"tunnel","capture_hosts":[],"hosts_profile":"测试"}`)
	var snap struct {
		File config.Values `json:"file"`
	}
	decodeJSON(t, res, &snap)
	if snap.File.HostsProfile != "测试" {
		t.Fatalf("hosts_profile = %q", snap.File.HostsProfile)
	}

	res = request(t, http.MethodGet, ts.URL+"/api/hosts")
	var list hostsListResponse
	decodeJSON(t, res, &list)
	if list.Active != "测试" {
		t.Fatalf("active = %q, want 测试", list.Active)
	}
	if profile := findProfile(t, list, "测试"); profile.Rules != 1 {
		t.Fatalf("rules = %d, want 1", profile.Rules)
	}

	// An existing profile whose body is invalid must be rejected with the
	// parse error, and the previously saved config must stay untouched.
	res = postBody(t, ts.URL+"/api/hosts", `{"name":"坏"}`)
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("create status = %d", res.StatusCode)
	}
	res.Body.Close()
	if err := os.WriteFile(filepath.Join(profiles.Dir(), "坏.hosts"), []byte("bad\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before, err = os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	res = putBody(t, ts.URL+"/api/config", `{"proxy":"127.0.0.1:8080","ui":"127.0.0.1:8081","max_flows":1000,"max_body":1048576,"theme":"dark","capture":false,"capture_empty":"none","capture_https":"tunnel","capture_hosts":[],"hosts_profile":"坏"}`)
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid profile status = %d, want 400", res.StatusCode)
	}
	var parseErr hostErrorResponse
	if err := json.NewDecoder(res.Body).Decode(&parseErr); err != nil {
		t.Fatal(err)
	}
	if parseErr.Error != "当前启用的 Hosts 配置无效" || parseErr.Line != 1 || !strings.HasPrefix(parseErr.Detail, "第 1 行：") {
		t.Fatalf("error = %+v, want documented activation error", parseErr)
	}
	after, err = os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("config file changed after invalid profile content")
	}
}

func TestPutConfigKeepsHostsWhenUnchanged(t *testing.T) {
	_, ts, _, profiles, _ := newHostsServer(t)

	res := postBody(t, ts.URL+"/api/hosts", `{"name":"测试"}`)
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("create status = %d", res.StatusCode)
	}
	res.Body.Close()
	res = putBody(t, hostURL(ts, "测试"), `{"content":"10.0.0.5 api.test\n"}`)
	res.Body.Close()
	res = putBody(t, ts.URL+"/api/hosts", `{"active":"测试"}`)
	res.Body.Close()

	// Break the active file behind the server's back. A theme-only save with
	// the same hosts_profile must not re-read or re-apply it.
	path := filepath.Join(profiles.Dir(), "测试.hosts")
	if err := os.WriteFile(path, []byte("bad\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res = request(t, http.MethodGet, ts.URL+"/api/hosts")
	var list hostsListResponse
	decodeJSON(t, res, &list)
	if list.ActiveError == "" {
		t.Fatal("active_error should report the broken active file")
	}

	res = putBody(t, ts.URL+"/api/config", `{"proxy":"127.0.0.1:8080","ui":"127.0.0.1:8081","max_flows":1000,"max_body":1048576,"theme":"light","capture":false,"capture_empty":"none","capture_https":"tunnel","capture_hosts":[],"hosts_profile":"测试"}`)
	var snap struct {
		File config.Values `json:"file"`
	}
	decodeJSON(t, res, &snap)
	if snap.File.Theme != "light" || snap.File.HostsProfile != "测试" {
		t.Fatalf("saved config = %+v", snap.File)
	}
}

func TestInitHostsMissingProfile(t *testing.T) {
	dir, file, err := config.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	file.HostsProfile = "missing"
	session := config.NewSession(dir, file, config.Overrides{}, file.Proxy, file.UI, file.MaxFlows, file.MaxBody)
	profiles, err := hosts.OpenProfiles(dir.Path())
	if err != nil {
		t.Fatal(err)
	}
	ca, err := cert.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	px := proxy.New("127.0.0.1:0", nil, file.MaxBody, ca)
	srv := api.New(store.New(file.MaxFlows), ca, session, px)
	srv.SetHostsProfiles(profiles)

	err = srv.InitHosts()
	if err == nil || !strings.Contains(err.Error(), filepath.Join(profiles.Dir(), "missing.hosts")) {
		t.Fatalf("InitHosts error = %v, want the missing.hosts path", err)
	}
	if !strings.Contains(err.Error(), "profile not found") {
		t.Fatalf("InitHosts error = %v, want profile not found", err)
	}

	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	res := request(t, http.MethodGet, ts.URL+"/api/hosts")
	var list hostsListResponse
	decodeJSON(t, res, &list)
	if list.Active != "" || list.Profiles == nil || len(list.Profiles) != 0 {
		t.Fatalf("hosts state = %+v, want no active and no profiles", list)
	}
}

func TestInitHostsInvalidProfile(t *testing.T) {
	dir, file, err := config.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	profiles, err := hosts.OpenProfiles(dir.Path())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(profiles.Dir(), "坏.hosts")
	if err := os.WriteFile(path, []byte("10.0.0.5 ok.test\nbad ip\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	file.HostsProfile = "坏"
	session := config.NewSession(dir, file, config.Overrides{}, file.Proxy, file.UI, file.MaxFlows, file.MaxBody)
	ca, err := cert.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	px := proxy.New("127.0.0.1:0", nil, file.MaxBody, ca)
	srv := api.New(store.New(file.MaxFlows), ca, session, px)
	srv.SetHostsProfiles(profiles)

	err = srv.InitHosts()
	if err == nil || !strings.Contains(err.Error(), path+": 2: invalid IP") {
		t.Fatalf("InitHosts error = %v, want %q", err, path+": 2: invalid IP")
	}
}

func TestHostsWritesRequireJSON(t *testing.T) {
	_, ts, _, profiles, _ := newHostsServer(t)

	// A text/plain body is a cross-origin simple request, so it must not be
	// allowed to create a profile.
	res := postContentType(t, ts.URL+"/api/hosts", "text/plain", `{"name":"csrf"}`)
	assertConfigError(t, res, http.StatusUnsupportedMediaType, "请求必须是 JSON")
	if _, err := profiles.Read("csrf"); !errors.Is(err, hosts.ErrNotFound) {
		t.Fatalf("csrf profile was created: %v", err)
	}

	res = postBody(t, ts.URL+"/api/hosts", `{"name":"原始"}`)
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("create status = %d, want 204", res.StatusCode)
	}
	res.Body.Close()

	res = postContentType(t, hostURL(ts, "原始")+"/rename", "text/plain", `{"name":"改名"}`)
	assertConfigError(t, res, http.StatusUnsupportedMediaType, "请求必须是 JSON")
	if _, err := profiles.Read("原始"); err != nil {
		t.Fatalf("rename source missing after rejected request: %v", err)
	}
	if _, err := profiles.Read("改名"); !errors.Is(err, hosts.ErrNotFound) {
		t.Fatalf("csrf rename created target: %v", err)
	}
}

func TestHostsRefreshOnList(t *testing.T) {
	_, ts, _, profiles, _ := newHostsServer(t)

	res := postBody(t, ts.URL+"/api/hosts", `{"name":"测试"}`)
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("create status = %d", res.StatusCode)
	}
	res.Body.Close()
	res = putBody(t, hostURL(ts, "测试"), `{"content":"10.0.0.5 one.test\n"}`)
	res.Body.Close()
	res = putBody(t, ts.URL+"/api/hosts", `{"active":"测试"}`)
	res.Body.Close()

	path := filepath.Join(profiles.Dir(), "测试.hosts")
	writeAt := func(body string, when time.Time) {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, when, when); err != nil {
			t.Fatal(err)
		}
	}
	writeAt("10.0.0.5 a.test\n10.0.0.6 b.test\n", time.Now().Add(2*time.Second))

	res = request(t, http.MethodGet, ts.URL+"/api/hosts")
	var list hostsListResponse
	decodeJSON(t, res, &list)
	if profile := findProfile(t, list, "测试"); profile.Rules != 2 {
		t.Fatalf("rules = %d, want 2 after external edit", profile.Rules)
	}
	if list.ActiveError != "" {
		t.Fatalf("active_error = %q, want empty", list.ActiveError)
	}

	writeAt("bad\n", time.Now().Add(4*time.Second))
	res = request(t, http.MethodGet, ts.URL+"/api/hosts")
	decodeJSON(t, res, &list)
	if list.ActiveError == "" {
		t.Fatal("active_error should report the external parse error")
	}

	// An external delete leaves the last-good count on the synthetic active
	// entry instead of reporting 0 rules.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	res = request(t, http.MethodGet, ts.URL+"/api/hosts")
	decodeJSON(t, res, &list)
	profile := findProfile(t, list, "测试")
	if profile.Rules != 2 {
		t.Fatalf("rules = %d, want last-good 2 after external delete", profile.Rules)
	}
	if profile.Error == "" || list.ActiveError == "" {
		t.Fatalf("active error missing after delete: profile=%q active=%q", profile.Error, list.ActiveError)
	}
}

func jsonString(s string) string {
	data, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(data)
}
