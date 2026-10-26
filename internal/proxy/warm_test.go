package proxy

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
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
	if err := fetchOK(client, rawURL); err != nil {
		t.Fatal(err)
	}
}

func fetchOK(client *http.Client, rawURL string) error {
	resp, err := client.Get(rawURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status = %d", resp.StatusCode)
	}
	return nil
}

// handlerGate holds each wave's handlers until all of them are inside the
// request, so none can finish and return its upstream connection to the pool
// before the other dials have happened.
type handlerGate struct {
	need    int
	mu      sync.Mutex
	entered int
	release chan struct{}
}

func newHandlerGate(n int) *handlerGate {
	return &handlerGate{need: n, release: make(chan struct{})}
}

func (g *handlerGate) arrive() {
	g.mu.Lock()
	g.entered++
	ch := g.release
	if g.entered == g.need {
		close(ch)
	}
	g.mu.Unlock()
	<-ch
}

func (g *handlerGate) reset() {
	g.mu.Lock()
	g.entered = 0
	g.release = make(chan struct{})
	g.mu.Unlock()
}

func TestIdleConnsReuseSixHTTP1(t *testing.T) {
	const waveSize = 6
	gate := newHandlerGate(waveSize)
	srv, count := http1TLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		gate.arrive()
		_, _ = io.WriteString(w, "ok")
	}))
	manager, pool := newCA(t)
	p := startProxy(t, &memSink{}, 1024, manager)
	client := trustClient(t, p.Addr(), pool, waveSize)
	// Block a seventh client connection while the previous handlers are still
	// unwinding, so the next wave reuses the idle upstream connections.
	client.Transport.(*http.Transport).MaxConnsPerHost = waveSize

	wave := func() error {
		gate.reset()
		var wg sync.WaitGroup
		errs := make([]error, waveSize)
		start := make(chan struct{})
		wg.Add(waveSize)
		for i := 0; i < waveSize; i++ {
			go func(i int) {
				defer wg.Done()
				<-start
				errs[i] = fetchOK(client, srv.URL)
			}(i)
		}
		close(start)
		wg.Wait()
		return errors.Join(errs...)
	}
	if err := wave(); err != nil {
		t.Fatal(err)
	}
	if got := count.n.Load(); got != waveSize {
		t.Fatalf("first wave accepts = %d, want %d", got, waveSize)
	}
	if err := wave(); err != nil {
		t.Fatal(err)
	}
	if got := count.n.Load(); got != waveSize {
		t.Fatalf("second wave accepts = %d, want %d", got, waveSize)
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

func TestUpstreamTLSResumesWhenKeepAliveIsOff(t *testing.T) {
	var mu sync.Mutex
	var resumed []bool
	srv, _ := http1TLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		resumed = append(resumed, r.TLS != nil && r.TLS.DidResume)
		mu.Unlock()
		_, _ = io.WriteString(w, "ok")
	}))
	manager, pool := newCA(t)
	sink := &memSink{}
	p := New("127.0.0.1:0", sink, 1024, manager)
	p.UpstreamTLS = &tls.Config{InsecureSkipVerify: true}
	p.upstreamDisableKeepAlive = true
	errCh := make(chan error, 1)
	go func() { errCh <- p.ListenAndServe() }()
	t.Cleanup(func() {
		_ = p.Close()
		if err := <-errCh; err != nil {
			t.Errorf("ListenAndServe: %v", err)
		}
	})
	client := trustClient(t, waitAddr(t, p), pool, 1)
	getOK(t, client, srv.URL)
	getOK(t, client, srv.URL)
	mu.Lock()
	defer mu.Unlock()
	if len(resumed) != 2 || resumed[0] || !resumed[1] {
		t.Fatalf("DidResume = %v, want [false true]", resumed)
	}
}
