package proxy

import (
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"

	"golang.org/x/net/http2"
)

type countingListener struct {
	net.Listener
	n atomic.Int32
}

func (l *countingListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err == nil {
		l.n.Add(1)
	}
	return c, err
}

func http1TLSServer(t *testing.T, h http.Handler) (*httptest.Server, *countingListener) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	count := &countingListener{Listener: ln}
	srv := httptest.NewUnstartedServer(h)
	srv.Listener = count
	srv.Config.TLSNextProto = map[string]func(*http.Server, *tls.Conn, http.Handler){}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv, count
}

func trustClient(t *testing.T, proxyAddr string, pool *x509.CertPool, idlePerHost int) *http.Client {
	t.Helper()
	proxyURL, err := url.Parse("http://" + proxyAddr)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{Transport: &http.Transport{
		Proxy:               http.ProxyURL(proxyURL),
		TLSClientConfig:     &tls.Config{RootCAs: pool},
		MaxIdleConns:        idlePerHost,
		MaxIdleConnsPerHost: idlePerHost,
		DisableCompression:  true,
	}}
}

func getOK(t *testing.T, client *http.Client, rawURL string) {
	t.Helper()
	resp, err := client.Get(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestIdleConnsReuseSixHTTP1(t *testing.T) {
	srv, count := http1TLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	manager, pool := newCA(t)
	p := startProxy(t, &memSink{}, 1024, manager)
	client := trustClient(t, p.Addr(), pool, 6)

	wave := func() {
		var wg sync.WaitGroup
		start := make(chan struct{})
		wg.Add(6)
		for i := 0; i < 6; i++ {
			go func() {
				defer wg.Done()
				<-start
				getOK(t, client, srv.URL)
			}()
		}
		close(start)
		wg.Wait()
	}
	wave()
	if got := count.n.Load(); got != 6 {
		t.Fatalf("first wave accepts = %d, want 6", got)
	}
	wave()
	if got := count.n.Load(); got != 6 {
		t.Fatalf("second wave accepts = %d, want 6", got)
	}
}

func TestSequentialHTTP1UsesOneUpstream(t *testing.T) {
	srv, count := http1TLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	manager, pool := newCA(t)
	p := startProxy(t, &memSink{}, 1024, manager)
	client := trustClient(t, p.Addr(), pool, 1)
	for i := 0; i < 5; i++ {
		getOK(t, client, srv.URL)
	}
	if got := count.n.Load(); got != 1 {
		t.Fatalf("accepts = %d, want 1", got)
	}
}

func TestSequentialHTTP2UsesOneUpstream(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	count := &countingListener{Listener: ln}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	srv.Listener = count
	srv.TLS = &tls.Config{NextProtos: []string{"h2"}}
	if err := http2.ConfigureServer(srv.Config, &http2.Server{}); err != nil {
		t.Fatal(err)
	}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	manager, pool := newCA(t)
	p := startProxy(t, &memSink{}, 1024, manager)
	client := trustClient(t, p.Addr(), pool, 1)
	for i := 0; i < 5; i++ {
		getOK(t, client, srv.URL)
	}
	if got := count.n.Load(); got != 1 {
		t.Fatalf("accepts = %d, want 1", got)
	}
}
