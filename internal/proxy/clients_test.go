package proxy

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"golang.org/x/net/http2"

	"github.com/zhangyw-cn/httpproxy/internal/capture"
	"github.com/zhangyw-cn/httpproxy/internal/clients"
	"github.com/zhangyw-cn/httpproxy/internal/hosts"
)

func TestProxyRecordsClientsPlainHTTP(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "plain-ok")
	}))
	defer upstream.Close()

	parsed, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	table, _, err := hosts.Parse([]byte("127.0.0.1 api.example.com\n"))
	if err != nil {
		t.Fatal(err)
	}

	p := startProxy(t, &memSink{}, 0, nil)
	p.SetHosts("clients-test", table)
	tracker := clients.New()
	p.SetClientTracker(tracker)

	client := proxyClient(t, p.Addr())
	resp, err := client.Get("http://api.example.com:" + parsed.Port() + "/hello")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK || string(body) != "plain-ok" {
		t.Fatalf("response = %d, %q", resp.StatusCode, body)
	}

	got := waitClient(t, tracker, "127.0.0.1")
	if got.Requests < 1 {
		t.Fatalf("requests = %d", got.Requests)
	}
	if got.Tunnels != 0 {
		t.Fatalf("plain HTTP counted as tunnel: %+v", got)
	}
	if !hasClientHost(got.Hosts, "api.example.com") {
		t.Fatalf("hosts = %+v", got.Hosts)
	}
}

func TestProxyRecordsClientsTunnel(t *testing.T) {
	echo := startEchoServer(t)
	_, port, err := net.SplitHostPort(echo.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	table, _, err := hosts.Parse([]byte("127.0.0.1 tunnel.example.com\n"))
	if err != nil {
		t.Fatal(err)
	}

	p := startProxy(t, &memSink{}, 0, nil)
	p.SetHosts("clients-test", table)
	policy, err := capture.Compile(false, "all", "tunnel", nil)
	if err != nil {
		t.Fatal(err)
	}
	p.SetPolicy(policy)
	tracker := clients.New()
	p.SetClientTracker(tracker)

	conn, err := net.DialTimeout("tcp", p.Addr(), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

	target := net.JoinHostPort("tunnel.example.com", port)
	if _, err := io.WriteString(conn, "CONNECT "+target+" HTTP/1.1\r\nHost: "+target+"\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(conn)
	resp, err := http.ReadResponse(reader, &http.Request{Method: http.MethodConnect})
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("CONNECT status = %d", resp.StatusCode)
	}

	got := waitClient(t, tracker, "127.0.0.1")
	if got.Tunnels != 1 {
		t.Fatalf("tunnels = %d", got.Tunnels)
	}
	if got.Requests != 0 {
		t.Fatalf("tunnel double-counted as request: %+v", got)
	}
	if !hasClientHost(got.Hosts, "tunnel.example.com") {
		t.Fatalf("hosts = %+v", got.Hosts)
	}

	// The tunnel really forwards bytes, not just a 200.
	if _, err := io.WriteString(conn, "ping"); err != nil {
		t.Fatal(err)
	}
	payload := make([]byte, 4)
	if _, err := io.ReadFull(reader, payload); err != nil {
		t.Fatal(err)
	}
	if string(payload) != "ping" {
		t.Fatalf("echoed %q", payload)
	}
}

func TestProxyRecordsClientsMITMConnect(t *testing.T) {
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "mitm-ok")
	}))
	defer upstream.Close()
	manager, pool := newCA(t)
	p := startProxy(t, &memSink{}, 0, manager)
	tracker := clients.New()
	p.SetClientTracker(tracker)

	target := upstream.Listener.Addr().String()
	host, _, err := net.SplitHostPort(target)
	if err != nil {
		t.Fatal(err)
	}
	tlsConn, err := dialMITM(t, p.Addr(), target, &tls.Config{
		RootCAs:    pool,
		ServerName: host,
		NextProtos: []string{"http/1.1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodGet, "https://"+target+"/mitm", nil)
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
	if resp.StatusCode != http.StatusOK || string(body) != "mitm-ok" {
		t.Fatalf("response = %d, %q", resp.StatusCode, body)
	}

	got := waitClient(t, tracker, "127.0.0.1")
	if got.Tunnels != 1 {
		t.Fatalf("tunnels = %d", got.Tunnels)
	}
	if got.Requests != 1 {
		t.Fatalf("requests = %d", got.Requests)
	}
	if !hasClientHost(got.Hosts, host) {
		t.Fatalf("hosts = %+v", got.Hosts)
	}
}

func TestProxyRecordsClientsHTTP2Streams(t *testing.T) {
	upstream := newH2TLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "h2-ok")
	}))
	defer upstream.Close()
	manager, pool := newCA(t)
	p := startProxy(t, &memSink{}, 0, manager)
	tracker := clients.New()
	p.SetClientTracker(tracker)

	transport := &http.Transport{
		Proxy:             http.ProxyURL(proxyURL(t, p)),
		TLSClientConfig:   &tls.Config{RootCAs: pool, NextProtos: []string{"h2"}},
		ForceAttemptHTTP2: true,
	}
	if err := http2.ConfigureTransport(transport); err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: transport, Timeout: 2 * time.Second}
	for i := 0; i < 2; i++ {
		resp, err := client.Get(upstream.URL + fmt.Sprintf("/h2-%d", i))
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != http.StatusOK || string(body) != "h2-ok" {
			t.Fatalf("response = %d, %q", resp.StatusCode, body)
		}
	}

	got := waitClient(t, tracker, "127.0.0.1")
	if got.Tunnels != 1 {
		t.Fatalf("tunnels = %d", got.Tunnels)
	}
	if got.Requests != 2 {
		t.Fatalf("requests = %d", got.Requests)
	}
}

func waitClient(t *testing.T, tracker *clients.Tracker, ip string) clients.Client {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, c := range tracker.Snapshot() {
			if c.IP == ip {
				return c
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("client %q not tracked: %+v", ip, tracker.Snapshot())
	return clients.Client{}
}

func hasClientHost(hosts []string, want string) bool {
	for _, host := range hosts {
		if host == want {
			return true
		}
	}
	return false
}

func startEchoServer(t *testing.T) net.Listener {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				_, _ = io.Copy(conn, conn)
			}()
		}
	}()
	return listener
}
