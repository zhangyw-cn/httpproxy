package proxy

import (
	"bufio"
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
	"time"

	"golang.org/x/net/http2"

	"httpproxy/internal/flow"
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

func TestSNIMismatchReusesUpstream(t *testing.T) {
	srv, count := http1TLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "sni-ok")
	}))
	manager, pool := newCA(t)
	p := startProxy(t, &memSink{}, 1024, manager)
	target := srv.Listener.Addr().String()
	for i := 0; i < 2; i++ {
		conn, err := dialMITM(t, p.Addr(), target, &tls.Config{
			RootCAs:    pool,
			ServerName: "name.example",
			NextProtos: []string{"http/1.1"},
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(conn, "GET / HTTP/1.1\r\nHost: name.example\r\nConnection: close\r\n\r\n"); err != nil {
			t.Fatal(err)
		}
		resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		_ = conn.Close()
		if err != nil {
			t.Fatal(err)
		}
		if string(body) != "sni-ok" {
			t.Fatalf("body = %q", body)
		}
	}
	if got := count.n.Load(); got != 1 {
		t.Fatalf("upstream accepts = %d, want 1", got)
	}
}

func TestSNIHandshakeTimesOut(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	accepted := make(chan net.Conn, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		accepted <- c
	}()
	t.Cleanup(func() {
		_ = ln.Close()
		select {
		case c := <-accepted:
			_ = c.Close()
		default:
		}
	})

	manager, pool := newCA(t)
	p := startProxy(t, &memSink{}, 1024, manager)
	conn, err := dialMITM(t, p.Addr(), ln.Addr().String(), &tls.Config{
		RootCAs:    pool,
		ServerName: "name.example",
		NextProtos: []string{"http/1.1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	// dialMITM leaves a 5s deadline on the client conn. The upstream handshake
	// is allowed 10s, so clear it before sending the request.
	if err := conn.SetDeadline(time.Time{}); err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	if _, err := io.WriteString(conn, "GET / HTTP/1.1\r\nHost: name.example\r\nConnection: close\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", resp.StatusCode)
	}
	if elapsed >= 15*time.Second {
		t.Fatalf("response took %s, want under 15s", elapsed)
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
}

type gateSink struct {
	mu      sync.Mutex
	order   []string
	updated []flow.Flow
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (s *gateSink) OnCreated(f flow.Flow) {
	s.mu.Lock()
	s.order = append(s.order, "c:"+f.ID)
	s.mu.Unlock()
}

func (s *gateSink) OnUpdated(f flow.Flow) {
	s.once.Do(func() {
		close(s.entered)
		<-s.release
	})
	s.mu.Lock()
	s.order = append(s.order, "u:"+f.ID)
	s.updated = append(s.updated, f)
	s.mu.Unlock()
}

func TestSecondRequestReturnsWhileUpdateIsBlocked(t *testing.T) {
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, r.URL.Path)
	}))
	t.Cleanup(upstream.Close)
	manager, pool := newCA(t)
	sink := &gateSink{entered: make(chan struct{}), release: make(chan struct{})}
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(sink.release) }) }
	p := New("127.0.0.1:0", sink, 1024, manager)
	p.UpstreamTLS = &tls.Config{InsecureSkipVerify: true}
	errCh := make(chan error, 1)
	go func() { errCh <- p.ListenAndServe() }()
	defer func() {
		release()
		_ = p.Close()
		if err := <-errCh; err != nil {
			t.Errorf("ListenAndServe: %v", err)
		}
	}()
	client := trustClient(t, waitAddr(t, p), pool, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		getOK(t, client, upstream.URL+"/one")
		getOK(t, client, upstream.URL+"/two")
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("second request waited for OnUpdated")
	}
	release()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		sink.mu.Lock()
		n := len(sink.updated)
		sink.mu.Unlock()
		if n >= 2 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	sink.mu.Lock()
	defer sink.mu.Unlock()
	if len(sink.updated) != 2 {
		t.Fatalf("updated = %d, want 2", len(sink.updated))
	}
	got := map[string]string{}
	for _, f := range sink.updated {
		got[f.Path] = string(f.RespBody)
	}
	if got["/one"] != "/one" || got["/two"] != "/two" {
		t.Fatalf("bodies = %v", got)
	}
}

func TestFullQueueRecordsTheNewFlowInline(t *testing.T) {
	holdFirst := make(chan struct{})
	started := make(chan struct{})
	var startOnce sync.Once
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/first" {
			startOnce.Do(func() { close(started) })
			<-holdFirst
		}
		_, _ = io.WriteString(w, r.URL.Path)
	}))
	var openUpstream sync.Once
	releaseUpstream := func() { openUpstream.Do(func() { close(holdFirst) }) }
	t.Cleanup(func() {
		releaseUpstream()
		srv.Close()
	})
	manager, pool := newCA(t)
	sink := &gateSink{entered: make(chan struct{}), release: make(chan struct{})}
	close(sink.release)
	p := New("127.0.0.1:0", sink, 8, manager)
	p.UpstreamTLS = &tls.Config{InsecureSkipVerify: true}
	p.recordQueue = 1
	p.recordGate = make(chan struct{})
	var openGate sync.Once
	releaseGate := func() { openGate.Do(func() { close(p.recordGate) }) }
	errCh := make(chan error, 1)
	go func() { errCh <- p.ListenAndServe() }()
	t.Cleanup(func() {
		releaseGate()
		releaseUpstream()
		_ = p.Close()
		if err := <-errCh; err != nil {
			t.Errorf("ListenAndServe: %v", err)
		}
	})
	client := trustClient(t, waitAddr(t, p), pool, 2)
	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		getOK(t, client, srv.URL+"/first")
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("first request did not reach the upstream")
	}
	getOK(t, client, srv.URL+"/second")
	sink.mu.Lock()
	var secondID string
	var secondBody string
	for _, f := range sink.updated {
		if f.Path == "/second" {
			secondID = f.ID
			secondBody = string(f.RespBody)
		}
	}
	order := append([]string(nil), sink.order...)
	sink.mu.Unlock()
	if secondID == "" || secondBody != "/second" {
		t.Fatalf("second flow id=%q body=%q", secondID, secondBody)
	}
	if indexOf(order, "c:"+secondID) < 0 || indexOf(order, "c:"+secondID) > indexOf(order, "u:"+secondID) {
		t.Fatalf("second order = %v", order)
	}
	releaseGate()
	releaseUpstream()
	select {
	case <-firstDone:
	case <-time.After(2 * time.Second):
		t.Fatal("first request did not finish after the gate opened")
	}
	deadline := time.Now().Add(2 * time.Second)
	var firstID string
	for time.Now().Before(deadline) {
		sink.mu.Lock()
		order = append([]string(nil), sink.order...)
		for _, f := range sink.updated {
			if f.Path == "/first" && string(f.RespBody) == "/first" {
				firstID = f.ID
			}
		}
		sink.mu.Unlock()
		if firstID != "" {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if firstID == "" || indexOf(order, "c:"+firstID) < 0 || indexOf(order, "c:"+firstID) > indexOf(order, "u:"+firstID) {
		t.Fatalf("first id %q order %v", firstID, order)
	}
}

func indexOf(items []string, want string) int {
	for i, item := range items {
		if item == want {
			return i
		}
	}
	return -1
}

func TestCloseWaitsForHTTP2Stream(t *testing.T) {
	entered := make(chan struct{})
	var once sync.Once
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() { close(entered) })
		<-r.Context().Done()
	}))
	defer upstream.Close()

	manager, pool := newCA(t)
	sink := &memSink{}
	p := startProxy(t, sink, 1024, manager)

	proxyURL, err := url.Parse("http://" + p.Addr())
	if err != nil {
		t.Fatal(err)
	}
	transport := &http.Transport{
		Proxy:             http.ProxyURL(proxyURL),
		TLSClientConfig:   &tls.Config{RootCAs: pool},
		ForceAttemptHTTP2: true,
	}
	if err := http2.ConfigureTransport(transport); err != nil {
		t.Fatal(err)
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport}

	getErr := make(chan error, 1)
	go func() {
		resp, err := client.Get(upstream.URL + "/stall")
		if err != nil {
			getErr <- err
			return
		}
		_, err = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		getErr <- err
	}()

	select {
	case <-entered:
	case err := <-getErr:
		t.Fatalf("client finished before the upstream handler entered: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("upstream handler did not enter")
	}

	_ = p.Close()

	if _, updated := sink.counts(); updated < 1 {
		t.Fatal("Close returned before OnUpdated")
	}

	select {
	case <-getErr:
	case <-time.After(5 * time.Second):
		t.Fatal("client Get did not return after Close")
	}
}
