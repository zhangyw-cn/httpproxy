package api_test

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"httpproxy/internal/clients"
	"httpproxy/internal/config"
	"httpproxy/internal/flow"
)

type overviewResponse struct {
	Proxy         string           `json:"proxy"`
	UI            string           `json:"ui"`
	StartedAt     time.Time        `json:"started_at"`
	UptimeSeconds int64            `json:"uptime_seconds"`
	Capture       bool             `json:"capture"`
	FlowCount     int              `json:"flow_count"`
	HostsProfile  string           `json:"hosts_profile"`
	HostsRules    int              `json:"hosts_rules"`
	HostsError    string           `json:"hosts_error"`
	Clients       []clients.Client `json:"clients"`
}

func getOverview(t *testing.T, url string) overviewResponse {
	t.Helper()
	res := request(t, http.MethodGet, url)
	var body overviewResponse
	decodeJSON(t, res, &body)
	return body
}

func TestOverview(t *testing.T) {
	before := time.Now()
	srv, ts, _, _, _ := newHostsServer(t)
	tracker := clients.New()
	srv.SetClients(tracker)
	tracker.Request("127.0.0.1", "api.test")
	tracker.Tunnel("127.0.0.1", "tunnel.test")

	srv.Sink().OnCreated(flow.Flow{ID: "f1", Host: "api.test"})
	srv.Sink().OnCreated(flow.Flow{ID: "f2", Host: "cdn.test"})

	res := postBody(t, ts.URL+"/api/hosts", `{"name":"测试"}`)
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("create status = %d", res.StatusCode)
	}
	res.Body.Close()
	res = putBody(t, hostURL(ts, "测试"), `{"content":"10.0.0.5 api.test\n10.0.0.6 cdn.test\n"}`)
	res.Body.Close()
	res = putBody(t, ts.URL+"/api/hosts", `{"active":"测试"}`)
	res.Body.Close()
	res = putBody(t, ts.URL+"/api/capture", `{"capture":true}`)
	decodeJSON(t, res, &struct{}{})

	got := getOverview(t, ts.URL+"/api/overview")

	defaults := config.Defaults()
	if got.Proxy != defaults.Proxy {
		t.Fatalf("proxy = %q, want %q", got.Proxy, defaults.Proxy)
	}
	if got.UI != defaults.UI {
		t.Fatalf("ui = %q, want %q", got.UI, defaults.UI)
	}
	if got.StartedAt.IsZero() || got.StartedAt.Before(before) || got.StartedAt.After(time.Now()) {
		t.Fatalf("started_at = %v, want between %v and now", got.StartedAt, before)
	}
	if got.UptimeSeconds < 0 || got.UptimeSeconds > int64(time.Since(got.StartedAt).Seconds()) {
		t.Fatalf("uptime_seconds = %d, started_at = %v", got.UptimeSeconds, got.StartedAt)
	}
	if !got.Capture {
		t.Fatal("capture = false, want true")
	}
	if got.FlowCount != 2 {
		t.Fatalf("flow_count = %d, want 2", got.FlowCount)
	}
	if got.HostsProfile != "测试" || got.HostsRules != 2 {
		t.Fatalf("hosts = %q/%d, want 测试/2", got.HostsProfile, got.HostsRules)
	}
	if got.HostsError != "" {
		t.Fatalf("hosts_error = %q, want empty", got.HostsError)
	}
	if len(got.Clients) != 1 {
		t.Fatalf("clients = %#v, want one", got.Clients)
	}
	client := got.Clients[0]
	if client.IP != "127.0.0.1" || client.Requests != 1 || client.Tunnels != 1 {
		t.Fatalf("client = %+v", client)
	}
	if client.LastSeen.IsZero() {
		t.Fatalf("client last_seen = %v", client.LastSeen)
	}
	if len(client.Hosts) != 2 || client.Hosts[0] != "tunnel.test" || client.Hosts[1] != "api.test" {
		t.Fatalf("client hosts = %#v", client.Hosts)
	}
}

func TestOverviewWithoutClients(t *testing.T) {
	_, ts, _, _, _ := newHostsServer(t)
	got := getOverview(t, ts.URL+"/api/overview")
	if got.Clients == nil {
		t.Fatal("clients must be [] not null")
	}
	if len(got.Clients) != 0 {
		t.Fatalf("clients = %#v, want empty", got.Clients)
	}
}

func TestOverviewRefreshesHosts(t *testing.T) {
	_, ts, _, profiles, _ := newHostsServer(t)

	res := postBody(t, ts.URL+"/api/hosts", `{"name":"测试"}`)
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("create status = %d", res.StatusCode)
	}
	res.Body.Close()
	res = putBody(t, hostURL(ts, "测试"), `{"content":"10.0.0.5 a.test\n10.0.0.6 b.test\n"}`)
	res.Body.Close()
	res = putBody(t, ts.URL+"/api/hosts", `{"active":"测试"}`)
	res.Body.Close()

	path := filepath.Join(profiles.Dir(), "测试.hosts")
	if err := os.WriteFile(path, []byte("bad\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}

	got := getOverview(t, ts.URL+"/api/overview")
	if got.HostsError == "" {
		t.Fatal("hosts_error should report the broken active file")
	}
	if got.HostsProfile != "测试" || got.HostsRules != 2 {
		t.Fatalf("hosts = %q/%d, want last good 测试/2", got.HostsProfile, got.HostsRules)
	}
}
