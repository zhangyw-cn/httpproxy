package proxy

import (
	"bufio"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"httpproxy/internal/capture"
	"httpproxy/internal/hosts"
)

func mustHostsTable(t *testing.T, body string) hosts.Table {
	t.Helper()
	table, _, err := hosts.Parse([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	return table
}

func TestHostsOverridePlainHTTP(t *testing.T) {
	var gotHost string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHost = r.Host
		_, _ = io.WriteString(w, "mapped")
	}))
	defer upstream.Close()
	_, port, _ := net.SplitHostPort(upstream.Listener.Addr().String())

	table := mustHostsTable(t, "127.0.0.1 api.test\n")

	sink := &memSink{}
	p := startProxy(t, sink, 1024, nil)
	p.SetHosts("测试", table)

	resp, err := proxyClient(t, p.Addr()).Get("http://api.test:" + port + "/x")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(body) != "mapped" {
		t.Fatalf("%d %q", resp.StatusCode, body)
	}
	if gotHost != "api.test:"+port {
		t.Fatalf("Host = %q", gotHost)
	}

	f := waitUpdatedFlow(t, sink)
	if f.UpstreamAddr != "127.0.0.1:"+port || f.HostsProfile != "测试" {
		t.Fatalf("upstream=%q profile=%q", f.UpstreamAddr, f.HostsProfile)
	}
}

func TestHostsOverrideHTTPSKeepsSNI(t *testing.T) {
	var gotSNI, gotHost string
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSNI = r.TLS.ServerName
		gotHost = r.Host
		_, _ = io.WriteString(w, "mapped-tls")
	}))
	defer upstream.Close()
	_, port, _ := net.SplitHostPort(upstream.Listener.Addr().String())
	target := "api.test:" + port

	table := mustHostsTable(t, "127.0.0.1 api.test\n")
	manager, pool := newCA(t)
	sink := &memSink{}
	p := startProxy(t, sink, 1024, manager)
	p.SetHosts("测试", table)

	tlsConn, err := dialMITM(t, p.Addr(), target, &tls.Config{
		RootCAs:    pool,
		ServerName: "api.test",
		NextProtos: []string{"http/1.1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(tlsConn, "GET /tls HTTP/1.1\r\nHost: "+target+"\r\nConnection: close\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	resp, err := http.ReadResponse(bufio.NewReader(tlsConn), &http.Request{Method: http.MethodGet})
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK || string(body) != "mapped-tls" {
		t.Fatalf("%d %q", resp.StatusCode, body)
	}
	if gotSNI != "api.test" {
		t.Fatalf("upstream SNI = %q, want original domain", gotSNI)
	}
	if gotHost != target {
		t.Fatalf("upstream Host = %q", gotHost)
	}

	f := waitUpdatedFlow(t, sink)
	if f.UpstreamAddr != "127.0.0.1:"+port || f.HostsProfile != "测试" {
		t.Fatalf("upstream=%q profile=%q", f.UpstreamAddr, f.HostsProfile)
	}
}

func TestHostsOverrideTunnel(t *testing.T) {
	var gotSNI string
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSNI = r.TLS.ServerName
		_, _ = io.WriteString(w, "tunneled")
	}))
	defer upstream.Close()
	_, port, _ := net.SplitHostPort(upstream.Listener.Addr().String())
	target := "api.test:" + port

	table := mustHostsTable(t, "127.0.0.1 api.test\n")
	sink := &memSink{}
	p := startProxy(t, sink, 1024, nil)
	// Capture is on but no host is selected, so HTTPS is tunneled end to end.
	policy, err := capture.Compile(true, "none", "tunnel", nil)
	if err != nil {
		t.Fatal(err)
	}
	p.SetPolicy(policy)
	p.SetHosts("测试", table)

	tlsConn, err := dialMITM(t, p.Addr(), target, &tls.Config{
		InsecureSkipVerify: true, // the client verifies the upstream, not the proxy
		ServerName:         "api.test",
		NextProtos:         []string{"http/1.1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodGet, "https://"+target+"/tunnel", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := req.Write(tlsConn); err != nil {
		t.Fatal(err)
	}
	resp, err := http.ReadResponse(bufio.NewReader(tlsConn), req)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK || string(body) != "tunneled" {
		t.Fatalf("%d %q", resp.StatusCode, body)
	}
	if gotSNI != "api.test" {
		t.Fatalf("upstream SNI = %q, want original domain", gotSNI)
	}
	if created, updated := sink.counts(); created != 0 || updated != 0 {
		t.Fatalf("tunnel recorded flows: created=%d updated=%d", created, updated)
	}
}

func TestHostsSelfProxyRejected(t *testing.T) {
	sink := &memSink{}
	p := startProxy(t, sink, 1024, nil)
	_, proxyPort, err := net.SplitHostPort(p.Addr())
	if err != nil {
		t.Fatal(err)
	}
	p.SetHosts("测试", mustHostsTable(t, "127.0.0.1 loop.test\n"))

	resp, err := proxyClient(t, p.Addr()).Get("http://loop.test:" + proxyPort + "/")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", resp.StatusCode)
	}
}

func TestSetHostsClosesIdleConnections(t *testing.T) {
	// Both upstreams share one host:port key so a pooled idle connection would
	// be reused unless SetHosts closes it. The second loopback address provides
	// the same port with a different IP.
	serverA := newLoopbackUpstream(t, "127.0.0.1:0", "A")
	defer serverA.Close()
	_, port, err := net.SplitHostPort(serverA.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	serverB, err := tryLoopbackUpstream(net.JoinHostPort("127.0.0.2", port), "B")
	if err != nil {
		t.Skipf("loopback alias 127.0.0.2 is unavailable: %v", err)
	}
	defer serverB.Close()

	sink := &memSink{}
	p := startProxy(t, sink, 1024, nil)
	p.SetHosts("a", mustHostsTable(t, "127.0.0.1 api.test\n"))

	parsed, err := url.Parse("http://" + p.Addr())
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{
		Transport: &http.Transport{Proxy: http.ProxyURL(parsed)},
		Timeout:   2 * time.Second,
	}
	get := func() string {
		t.Helper()
		resp, err := client.Get("http://api.test:" + port + "/")
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		return string(body)
	}

	if got := get(); got != "A" {
		t.Fatalf("first response = %q, want A", got)
	}
	p.SetHosts("b", mustHostsTable(t, "127.0.0.2 api.test\n"))
	if got := get(); got != "B" {
		t.Fatalf("second response = %q, want B (stale idle upstream connection was reused)", got)
	}
}

func newLoopbackUpstream(t *testing.T, addr, body string) *httptest.Server {
	t.Helper()
	server, err := tryLoopbackUpstream(addr, body)
	if err != nil {
		t.Fatal(err)
	}
	return server
}

func tryLoopbackUpstream(addr, body string) (*httptest.Server, error) {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, body)
	}))
	_ = server.Listener.Close()
	server.Listener = listener
	server.Start()
	return server, nil
}
