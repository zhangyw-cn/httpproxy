package proxy

import (
	"bufio"
	"bytes"
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
	"time"

	"github.com/google/uuid"
	"golang.org/x/net/http2"

	"httpproxy/internal/cert"
	"httpproxy/internal/flow"
)

type memSink struct {
	mu      sync.Mutex
	created []flow.Flow
	updated []flow.Flow
}

func (s *memSink) OnCreated(f flow.Flow) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.created = append(s.created, f)
}

func (s *memSink) OnUpdated(f flow.Flow) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.updated = append(s.updated, f)
}

func (s *memSink) last() (flow.Flow, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.updated) == 0 {
		return flow.Flow{}, false
	}
	return s.updated[len(s.updated)-1], true
}

func (s *memSink) counts() (created, updated int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.created), len(s.updated)
}

func (s *memSink) firstCreated() (flow.Flow, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.created) == 0 {
		return flow.Flow{}, false
	}
	return s.created[0], true
}

func TestReadLimitedConsumesBodyAndReportsTotalSize(t *testing.T) {
	r := bytes.NewBufferString("abcdef")

	got, total, truncated, err := readLimited(r, 4)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "abcd" || total != 6 || !truncated {
		t.Fatalf("readLimited() = %q, %d, %t", got, total, truncated)
	}
	if r.Len() != 0 {
		t.Fatalf("reader has %d bytes remaining", r.Len())
	}
}

func TestProxyCleartextHTTPCapturesFlowAndFullBodies(t *testing.T) {
	var upstreamBody []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamBody, _ = io.ReadAll(r.Body)
		w.Header().Set("X-Up", "1")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("abcdef"))
	}))
	defer upstream.Close()

	sink := &memSink{}
	p := New("127.0.0.1:0", sink, 4, nil)
	errCh := make(chan error, 1)
	go func() { errCh <- p.ListenAndServe() }()
	t.Cleanup(func() {
		_ = p.Close()
		if err := <-errCh; err != nil {
			t.Errorf("ListenAndServe: %v", err)
		}
	})

	client := proxyClient(t, waitAddr(t, p))
	req, err := http.NewRequest(http.MethodPost, upstream.URL+"/q?x=1", bytes.NewBufferString("123456"))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Request", "yes")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != http.StatusCreated || string(body) != "abcdef" {
		t.Fatalf("response = %d, %q", resp.StatusCode, body)
	}
	if string(upstreamBody) != "123456" {
		t.Fatalf("upstream request body = %q", upstreamBody)
	}

	f := waitUpdatedFlow(t, sink)
	if _, err := uuid.Parse(f.ID); err != nil {
		t.Fatalf("flow ID %q is not a UUID: %v", f.ID, err)
	}
	createdFlow, ok := sink.firstCreated()
	if !ok {
		t.Fatal("created flow was not captured")
	}
	// Streaming tee: OnCreated may not yet include the request body.
	_ = createdFlow
	if f.Scheme != "http" || f.Method != http.MethodPost || f.Path != "/q?x=1" {
		t.Fatalf("request metadata: %+v", f)
	}
	if f.Host == "" || f.HTTPVersion != "1.1" || f.ClientAddr == "" {
		t.Fatalf("connection metadata: %+v", f)
	}
	if f.StatusCode != http.StatusCreated || headerValue(f.RespHeaders, "X-Up") != "1" {
		t.Fatalf("response metadata: %+v", f)
	}
	if string(f.ReqBody) != "1234" || f.ReqSize != 6 || !f.ReqTruncated {
		t.Fatalf("request capture: body=%q size=%d truncated=%t", f.ReqBody, f.ReqSize, f.ReqTruncated)
	}
	if string(f.RespBody) != "abcd" || f.RespSize != 6 || !f.RespTruncated {
		t.Fatalf("response capture: body=%q size=%d truncated=%t", f.RespBody, f.RespSize, f.RespTruncated)
	}
	if f.StartedAt.IsZero() || f.FinishedAt.IsZero() || f.FinishedAt.Before(f.StartedAt) || f.Error != "" {
		t.Fatalf("timing/error metadata: %+v", f)
	}
	created, updated := sink.counts()
	if created != 1 || updated != 1 {
		t.Fatalf("sink calls: created=%d updated=%d", created, updated)
	}
}

func TestProxyTransportLeavesUpstreamCompressionUntouched(t *testing.T) {
	var acceptEncoding string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		acceptEncoding = r.Header.Get("Accept-Encoding")
		w.Header().Set("Content-Encoding", "gzip")
		_, _ = io.WriteString(w, "compressed-view")
	}))
	defer upstream.Close()

	sink := &memSink{}
	p := New("127.0.0.1:0", sink, 1024, nil)
	if !p.upstream().DisableCompression {
		t.Fatal("upstream transport compression is enabled")
	}
	errCh := make(chan error, 1)
	go func() { errCh <- p.ListenAndServe() }()
	t.Cleanup(func() {
		_ = p.Close()
		if err := <-errCh; err != nil {
			t.Errorf("ListenAndServe: %v", err)
		}
	})

	client := proxyClient(t, waitAddr(t, p))
	client.Transport.(*http.Transport).DisableCompression = true
	resp, err := client.Get(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if acceptEncoding != "" {
		t.Fatalf("upstream Accept-Encoding = %q", acceptEncoding)
	}
	if resp.Header.Get("Content-Encoding") != "gzip" || string(body) != "compressed-view" {
		t.Fatalf("response was altered: encoding=%q body=%q", resp.Header.Get("Content-Encoding"), body)
	}
	f := waitUpdatedFlow(t, sink)
	if string(f.RespBody) != "compressed-view" {
		t.Fatalf("captured response body = %q", f.RespBody)
	}
}

func TestProxyUpstreamFailureUpdatesFlowAndReturnsBadGateway(t *testing.T) {
	unreachable := closedAddress(t)
	sink := &memSink{}
	p := New("127.0.0.1:0", sink, 1024, nil)
	errCh := make(chan error, 1)
	go func() { errCh <- p.ListenAndServe() }()
	t.Cleanup(func() {
		_ = p.Close()
		if err := <-errCh; err != nil {
			t.Errorf("ListenAndServe: %v", err)
		}
	})

	resp, err := proxyClient(t, waitAddr(t, p)).Get("http://" + unreachable + "/fail")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	f := waitUpdatedFlow(t, sink)
	if f.Error == "" || f.StatusCode != http.StatusBadGateway || f.FinishedAt.IsZero() {
		t.Fatalf("failed flow: %+v", f)
	}
}

func TestProxyHTTPSMITM(t *testing.T) {
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/secure" {
			t.Errorf("upstream path = %q", r.URL.Path)
		}
		_, _ = io.WriteString(w, "secure")
	}))
	defer upstream.Close()

	manager, err := cert.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sink := &memSink{}
	p := New("127.0.0.1:0", sink, 1024, manager)
	p.UpstreamTLS = &tls.Config{InsecureSkipVerify: true} // httptest uses a private test CA.
	errCh := make(chan error, 1)
	go func() { errCh <- p.ListenAndServe() }()
	t.Cleanup(func() {
		_ = p.Close()
		if err := <-errCh; err != nil {
			t.Errorf("ListenAndServe: %v", err)
		}
	})

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(manager.CACertPEM()) {
		t.Fatal("failed to trust proxy CA")
	}
	proxyURL, err := url.Parse("http://" + waitAddr(t, p))
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{
		Transport: &http.Transport{
			Proxy:             http.ProxyURL(proxyURL),
			DisableKeepAlives: true,
			TLSClientConfig:   &tls.Config{RootCAs: pool},
		},
		Timeout: 2 * time.Second,
	}
	resp, err := client.Get(upstream.URL + "/secure")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK || string(body) != "secure" {
		t.Fatalf("response = %d, %q", resp.StatusCode, body)
	}

	f := waitUpdatedFlow(t, sink)
	if f.Scheme != "https" || f.Method != http.MethodGet || f.Path != "/secure" {
		t.Fatalf("request metadata: %+v", f)
	}
	if f.HTTPVersion != "1.1" || f.StatusCode != http.StatusOK || string(f.RespBody) != "secure" {
		t.Fatalf("response flow: %+v body=%q", f, f.RespBody)
	}
}

func TestProxyHTTPSMITMUsesConnectAuthorityOverInnerHost(t *testing.T) {
	upstreamRequests := make(chan *http.Request, 1)
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamRequests <- r.Clone(r.Context())
		_, _ = io.WriteString(w, "connect-target")
	}))
	defer upstream.Close()
	upstreamURL, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}

	manager, err := cert.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sink := &memSink{}
	p := New("127.0.0.1:0", sink, 1024, manager)
	p.UpstreamTLS = &tls.Config{InsecureSkipVerify: true} // httptest uses a private test CA.
	errCh := make(chan error, 1)
	go func() { errCh <- p.ListenAndServe() }()
	t.Cleanup(func() {
		_ = p.Close()
		if err := <-errCh; err != nil {
			t.Errorf("ListenAndServe: %v", err)
		}
	})

	conn, err := net.DialTimeout("tcp", waitAddr(t, p), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_, err = io.WriteString(conn, "CONNECT "+upstreamURL.Host+" HTTP/1.1\r\nHost: "+upstreamURL.Host+"\r\n\r\n")
	if err != nil {
		t.Fatal(err)
	}
	connectResp, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: http.MethodConnect})
	if err != nil {
		t.Fatal(err)
	}
	_ = connectResp.Body.Close()
	if connectResp.StatusCode != http.StatusOK {
		t.Fatalf("CONNECT status = %d", connectResp.StatusCode)
	}

	host, _, err := net.SplitHostPort(upstreamURL.Host)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(manager.CACertPEM()) {
		t.Fatal("failed to trust proxy CA")
	}
	tlsConn := tls.Client(conn, &tls.Config{RootCAs: pool, ServerName: host})
	if err := tlsConn.Handshake(); err != nil {
		t.Fatal(err)
	}
	_, err = io.WriteString(tlsConn, "GET /secure?q=1 HTTP/1.1\r\nHost: override.invalid:9443\r\nConnection: close\r\n\r\n")
	if err != nil {
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
	if resp.StatusCode != http.StatusOK || string(body) != "connect-target" {
		t.Fatalf("response = %d, %q", resp.StatusCode, body)
	}

	select {
	case got := <-upstreamRequests:
		if got.URL.RequestURI() != "/secure?q=1" {
			t.Fatalf("upstream request URI = %q", got.URL.RequestURI())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("CONNECT target did not receive request")
	}
	f := waitUpdatedFlow(t, sink)
	if f.Host != upstreamURL.Host || f.Path != "/secure?q=1" {
		t.Fatalf("flow target/path: host=%q path=%q", f.Host, f.Path)
	}
}

func TestProxyHTTP2(t *testing.T) {
	var upstreamConnections atomic.Int32
	upstream := newH2TLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor != 2 {
			t.Errorf("upstream got proto %s", r.Proto)
		}
		_, _ = io.WriteString(w, "h2ok")
	}), func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			upstreamConnections.Add(1)
		}
	})
	defer upstream.Close()

	manager, err := cert.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sink := &memSink{}
	p := New("127.0.0.1:0", sink, 1024, manager)
	p.UpstreamTLS = &tls.Config{InsecureSkipVerify: true} // httptest uses a private test CA.
	errCh := make(chan error, 1)
	go func() { errCh <- p.ListenAndServe() }()
	t.Cleanup(func() {
		_ = p.Close()
		if err := <-errCh; err != nil {
			t.Errorf("ListenAndServe: %v", err)
		}
	})

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(manager.CACertPEM()) {
		t.Fatal("failed to trust proxy CA")
	}
	proxyURL, err := url.Parse("http://" + waitAddr(t, p))
	if err != nil {
		t.Fatal(err)
	}
	transport := &http.Transport{
		Proxy:             http.ProxyURL(proxyURL),
		TLSClientConfig:   &tls.Config{RootCAs: pool, NextProtos: []string{"h2"}},
		ForceAttemptHTTP2: true,
	}
	if err := http2.ConfigureTransport(transport); err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: transport, Timeout: 2 * time.Second}
	resp, err := client.Get(upstream.URL + "/h2")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "h2ok" {
		t.Fatalf("got %q", body)
	}
	resp, err = client.Get(upstream.URL + "/h2-again")
	if err != nil {
		t.Fatal(err)
	}
	_, err = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if got := upstreamConnections.Load(); got != 1 {
		t.Fatalf("upstream HTTP/2 connections = %d, want 1", got)
	}
	f := waitUpdatedFlow(t, sink)
	if f.HTTPVersion != "2" {
		t.Fatalf("want http version 2, got %q", f.HTTPVersion)
	}
}

func TestHTTP2ResponseCaptureFinishesAfterClientWriteFailure(t *testing.T) {
	upstream := newH2TLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ab")
		w.(http.Flusher).Flush()
		time.Sleep(20 * time.Millisecond)
		_, _ = io.WriteString(w, "cdef")
	}))
	defer upstream.Close()

	sink := &memSink{}
	p := New("", sink, 4, nil)
	p.UpstreamTLS = &tls.Config{InsecureSkipVerify: true} // httptest uses a private test CA.
	req := httptest.NewRequest(http.MethodGet, "https://"+upstream.Listener.Addr().String()+"/", nil)
	p.handleHTTP2Request(failingResponseWriter{header: make(http.Header)}, req, upstream.Listener.Addr().String(), "", true)

	f := waitUpdatedFlow(t, sink)
	if string(f.RespBody) != "abcd" || f.RespSize != 6 || !f.RespTruncated {
		t.Fatalf("response capture: body=%q size=%d truncated=%t", f.RespBody, f.RespSize, f.RespTruncated)
	}
	if f.Error == "" {
		t.Fatal("flow did not record the client write failure")
	}
}

type failingResponseWriter struct {
	header http.Header
}

func (w failingResponseWriter) Header() http.Header {
	return w.header
}

func (failingResponseWriter) WriteHeader(int) {}

func (failingResponseWriter) Write([]byte) (int, error) {
	return 0, io.ErrClosedPipe
}

func newH2TLSServer(t *testing.T, handler http.Handler, connState ...func(net.Conn, http.ConnState)) *httptest.Server {
	t.Helper()
	server := httptest.NewUnstartedServer(handler)
	server.TLS = &tls.Config{NextProtos: []string{"h2"}}
	if len(connState) > 0 {
		server.Config.ConnState = connState[0]
	}
	if err := http2.ConfigureServer(server.Config, &http2.Server{}); err != nil {
		t.Fatal(err)
	}
	server.StartTLS()
	return server
}

func proxyClient(t *testing.T, addr string) *http.Client {
	t.Helper()
	proxyURL, err := url.Parse("http://" + addr)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{
		Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL), DisableKeepAlives: true},
		Timeout:   2 * time.Second,
	}
}

func waitAddr(t *testing.T, p *Proxy) string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if addr := p.Addr(); addr != "" {
			return addr
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("proxy did not start listening")
	return ""
}

func waitUpdatedFlow(t *testing.T, sink *memSink) flow.Flow {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if f, ok := sink.last(); ok {
			return f
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("flow was not updated")
	return flow.Flow{}
}

func closedAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	_ = listener.Close()
	return addr
}

func headerValue(headers []flow.Header, name string) string {
	for _, header := range headers {
		if http.CanonicalHeaderKey(header.Name) == http.CanonicalHeaderKey(name) {
			return header.Value
		}
	}
	return ""
}
