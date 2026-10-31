package proxy

import (
	"bufio"
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/andybalholm/brotli"
	"golang.org/x/net/http2"

	"httpproxy/internal/capture"
	"httpproxy/internal/cert"
	"httpproxy/internal/flow"
)

func startProxy(t *testing.T, sink *memSink, maxBody int64, manager *cert.Manager) *Proxy {
	t.Helper()
	p := New("127.0.0.1:0", sink, maxBody, manager)
	// Only trust settings are injected; ALPN is left to the proxy.
	p.UpstreamTLS = &tls.Config{InsecureSkipVerify: true}
	errCh := make(chan error, 1)
	go func() { errCh <- p.ListenAndServe() }()
	t.Cleanup(func() {
		_ = p.Close()
		if err := <-errCh; err != nil {
			t.Errorf("ListenAndServe: %v", err)
		}
	})
	waitAddr(t, p)
	return p
}

func newCA(t *testing.T) (*cert.Manager, *x509.CertPool) {
	t.Helper()
	manager, err := cert.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(manager.CACertPEM()) {
		t.Fatal("failed to trust proxy CA")
	}
	return manager, pool
}

// dialMITM opens a CONNECT tunnel through the proxy and performs the client
// TLS handshake with the given ALPN offer.
func dialMITM(t *testing.T, proxyAddr, target string, tlsConfig *tls.Config) (*tls.Conn, error) {
	t.Helper()
	conn, err := net.DialTimeout("tcp", proxyAddr, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.WriteString(conn, "CONNECT "+target+" HTTP/1.1\r\nHost: "+target+"\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	resp, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: http.MethodConnect})
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("CONNECT status = %d", resp.StatusCode)
	}
	tlsConn := tls.Client(conn, tlsConfig)
	return tlsConn, tlsConn.Handshake()
}

func TestProxyHTTP1ClientToHTTP2Upstream(t *testing.T) {
	upstream := newH2TLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor != 2 {
			t.Errorf("upstream got proto %s, want HTTP/2", r.Proto)
		}
		body, _ := io.ReadAll(r.Body)
		// Flushing before the end forces a response without Content-Length.
		_, _ = io.WriteString(w, "part1-")
		w.(http.Flusher).Flush()
		_, _ = io.WriteString(w, r.URL.Path+"-"+string(body))
	}))
	defer upstream.Close()

	manager, pool := newCA(t)
	sink := &memSink{}
	p := startProxy(t, sink, 1024, manager)

	target := upstream.Listener.Addr().String()
	host, _, _ := net.SplitHostPort(target)
	tlsConn, err := dialMITM(t, p.Addr(), target, &tls.Config{RootCAs: pool, ServerName: host, NextProtos: []string{"http/1.1"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := tlsConn.ConnectionState().NegotiatedProtocol; got == "h2" {
		t.Fatalf("client ALPN = %q", got)
	}

	reader := bufio.NewReader(tlsConn)
	for i, path := range []string{"/one", "/two"} {
		_, err := io.WriteString(tlsConn, "POST "+path+" HTTP/1.1\r\nHost: "+target+"\r\nContent-Length: 3\r\n\r\nabc")
		if err != nil {
			t.Fatal(err)
		}
		statusLine, err := reader.Peek(len("HTTP/1.1 "))
		if err != nil {
			t.Fatal(err)
		}
		if string(statusLine) != "HTTP/1.1 " {
			t.Fatalf("request %d status line starts %q, want HTTP/1.1", i, statusLine)
		}
		resp, err := http.ReadResponse(reader, &http.Request{Method: http.MethodPost})
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			t.Fatalf("request %d body: %v", i, err)
		}
		if resp.StatusCode != http.StatusOK || string(body) != "part1-"+path+"-abc" {
			t.Fatalf("request %d response = %d %q", i, resp.StatusCode, body)
		}
		if resp.Close {
			t.Fatalf("request %d: proxy closed a keep-alive connection", i)
		}
	}

	f := waitFlowCount(t, sink, 2)
	if f.HTTPVersion != "1.1" || f.StatusCode != http.StatusOK || f.Error != "" {
		t.Fatalf("flow: %+v", f)
	}
}

func TestProxyHTTP2ClientToHTTP1OnlyUpstream(t *testing.T) {
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor != 1 {
			t.Errorf("upstream got proto %s, want HTTP/1.x", r.Proto)
		}
		_, _ = io.WriteString(w, "h1-upstream")
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
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}

	for i := 0; i < 2; i++ {
		resp, err := client.Get(upstream.URL + "/h1")
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if resp.ProtoMajor != 2 {
			t.Fatalf("client proto = %s, want HTTP/2", resp.Proto)
		}
		if resp.StatusCode != http.StatusOK || string(body) != "h1-upstream" {
			t.Fatalf("response = %d %q", resp.StatusCode, body)
		}
	}

	f := waitFlowCount(t, sink, 2)
	if f.HTTPVersion != "2" || f.StatusCode != http.StatusOK || f.Error != "" {
		t.Fatalf("flow: %+v", f)
	}
}

func TestProxyCleartextKeepAliveServesSequentialRequests(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "resp"+r.URL.Path)
	}))
	defer upstream.Close()

	sink := &memSink{}
	p := startProxy(t, sink, 1024, nil)
	conn, err := net.DialTimeout("tcp", p.Addr(), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

	reader := bufio.NewReader(conn)
	for _, path := range []string{"/a", "/b", "/c"} {
		_, err := io.WriteString(conn, "GET "+upstream.URL+path+" HTTP/1.1\r\nHost: "+upstream.Listener.Addr().String()+"\r\n\r\n")
		if err != nil {
			t.Fatal(err)
		}
		resp, err := http.ReadResponse(reader, &http.Request{Method: http.MethodGet})
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if string(body) != "resp"+path {
			t.Fatalf("%s: body = %q", path, body)
		}
	}
	waitFlowCount(t, sink, 3)
}

func TestProxyStreamsLargeRequestBodyWithBoundedCapture(t *testing.T) {
	const size = 1 << 20
	received := make(chan int, 2)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n, _ := io.Copy(io.Discard, r.Body)
		received <- int(n)
	}))
	defer upstream.Close()

	sink := &memSink{}
	p := startProxy(t, sink, 8, nil)
	client := proxyClient(t, p.Addr())

	payload := bytes.Repeat([]byte("x"), size)
	for i, chunked := range []bool{false, true} {
		var body io.Reader = bytes.NewReader(payload)
		if chunked {
			body = io.MultiReader(bytes.NewReader(payload))
		}
		resp, err := client.Post(upstream.URL, "application/octet-stream", body)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if got := <-received; got != size {
			t.Fatalf("chunked=%t upstream received %d bytes, want %d", chunked, got, size)
		}
		f := waitFlowCount(t, sink, i+1)
		if len(f.ReqBody) != 8 || f.ReqSize != size || !f.ReqTruncated {
			t.Fatalf("chunked=%t capture: len=%d size=%d truncated=%t", chunked, len(f.ReqBody), f.ReqSize, f.ReqTruncated)
		}
	}
}

func TestProxyTLSHandshakeFailureEmitsFlow(t *testing.T) {
	manager, _ := newCA(t)
	sink := &memSink{}
	p := startProxy(t, sink, 1024, manager)

	_, err := dialMITM(t, p.Addr(), "untrusted.example:443", &tls.Config{
		RootCAs:    x509.NewCertPool(),
		ServerName: "untrusted.example",
	})
	if err == nil {
		t.Fatal("client handshake unexpectedly succeeded")
	}
	f := waitUpdatedFlow(t, sink)
	if f.Method != http.MethodConnect || f.Host != "untrusted.example:443" || !strings.Contains(f.Error, "TLS handshake") {
		t.Fatalf("handshake failure flow: %+v", f)
	}
}

func TestProxyStoresDecodedCopyButForwardsCompressedBytes(t *testing.T) {
	var compressed bytes.Buffer
	gz := gzip.NewWriter(&compressed)
	_, _ = io.WriteString(gz, `{"hello":"world"}`)
	_ = gz.Close()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Encoding", "gzip")
		_, _ = w.Write(compressed.Bytes())
	}))
	defer upstream.Close()

	sink := &memSink{}
	p := startProxy(t, sink, 1024, nil)
	client := proxyClient(t, p.Addr())
	client.Transport.(*http.Transport).DisableCompression = true
	resp, err := client.Get(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if !bytes.Equal(body, compressed.Bytes()) {
		t.Fatal("client did not receive the original compressed bytes")
	}
	f := waitUpdatedFlow(t, sink)
	if !bytes.Equal(f.RespBody, compressed.Bytes()) {
		t.Fatal("raw captured body was altered")
	}
	if string(f.RespBodyDecoded) != `{"hello":"world"}` {
		t.Fatalf("decoded body = %q", f.RespBodyDecoded)
	}
}

func TestDecodeForDisplay(t *testing.T) {
	const text = "decoded text for display"
	encode := func(newWriter func(io.Writer) io.WriteCloser) []byte {
		var buf bytes.Buffer
		w := newWriter(&buf)
		_, _ = io.WriteString(w, text)
		_ = w.Close()
		return buf.Bytes()
	}
	cases := map[string][]byte{
		"gzip": encode(func(w io.Writer) io.WriteCloser { return gzip.NewWriter(w) }),
		"br":   encode(func(w io.Writer) io.WriteCloser { return brotli.NewWriter(w) }),
		"deflate": encode(func(w io.Writer) io.WriteCloser {
			return zlib.NewWriter(w)
		}),
	}
	rawDeflate := encode(func(w io.Writer) io.WriteCloser {
		fw, _ := flate.NewWriter(w, flate.DefaultCompression)
		return fw
	})
	for encoding, body := range cases {
		got := decodeForDisplay(body, http.Header{"Content-Encoding": {encoding}}, 1024)
		if string(got) != text {
			t.Errorf("%s: decoded %q", encoding, got)
		}
	}
	if got := decodeForDisplay(rawDeflate, http.Header{"Content-Encoding": {"deflate"}}, 1024); string(got) != text {
		t.Errorf("raw deflate: decoded %q", got)
	}
	if got := decodeForDisplay(cases["gzip"], http.Header{"Content-Encoding": {"gzip"}}, 7); string(got) != text[:7] {
		t.Errorf("decoded output not capped: %q", got)
	}
	if got := decodeForDisplay([]byte(text), http.Header{}, 1024); got != nil {
		t.Errorf("identity body produced decoded copy %q", got)
	}
}

func TestDecryptOnlyForwardsWithoutCapture(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "hello-body")
	}))
	defer upstream.Close()
	sink := &memSink{}
	p := startProxy(t, sink, 1024, nil)
	policy, err := capture.Compile(true, "none", "decrypt", nil)
	if err != nil {
		t.Fatal(err)
	}
	p.SetPolicy(policy)
	var captures atomic.Int64
	p.bodyCaptureHook = func() { captures.Add(1) }
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL(t, p))}}
	res, err := client.Get(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	got, _ := io.ReadAll(res.Body)
	if string(got) != "hello-body" || res.StatusCode != http.StatusOK {
		t.Fatalf("status %d body %q", res.StatusCode, got)
	}
	if created, updated := sink.counts(); created != 0 || updated != 0 {
		t.Fatalf("sink created=%d updated=%d", created, updated)
	}
	if captures.Load() != 0 {
		t.Fatal("decrypt-only allocated a body capture")
	}
}

func TestTunnelPassesBytesWithoutMITM(t *testing.T) {
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "tunneled")
	}))
	defer upstream.Close()
	manager, _ := newCA(t)
	sink := &memSink{}
	p := startProxy(t, sink, 1024, manager)
	policy, err := capture.Compile(true, "all", "tunnel", []string{"example.com"})
	if err != nil {
		t.Fatal(err)
	}
	p.SetPolicy(policy)
	target := strings.TrimPrefix(upstream.URL, "https://")
	conn, err := net.Dial("tcp", p.Addr())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := io.WriteString(conn, "CONNECT "+target+" HTTP/1.1\r\nHost: "+target+"\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(conn)
	status, err := reader.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(status, "200") {
		t.Fatalf("status %q", status)
	}
	for {
		line, err := reader.ReadString('\n')
		if err != nil || line == "\r\n" {
			break
		}
	}
	pool := upstream.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs
	tlsConn := tls.Client(&readerConn{Conn: conn, r: reader}, &tls.Config{RootCAs: pool, ServerName: "127.0.0.1"})
	if err := tlsConn.Handshake(); err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodGet, upstream.URL+"/", nil)
	if err := req.Write(tlsConn); err != nil {
		t.Fatal(err)
	}
	resp, err := http.ReadResponse(bufio.NewReader(tlsConn), req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if string(body) != "tunneled" {
		t.Fatalf("body %q", body)
	}
	if created, _ := sink.counts(); created != 0 {
		t.Fatalf("tunnel recorded %d flows", created)
	}
}

type readerConn struct {
	net.Conn
	r *bufio.Reader
}

func (c *readerConn) Read(p []byte) (int, error) { return c.r.Read(p) }

func proxyURL(t *testing.T, p *Proxy) *url.URL {
	t.Helper()
	parsed, err := url.Parse("http://" + p.Addr())
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func TestTunnelDialFailureSends502Without200(t *testing.T) {
	sink := &memSink{}
	p := startProxy(t, sink, 1024, nil)
	policy, err := capture.Compile(false, "all", "tunnel", nil)
	if err != nil {
		t.Fatal(err)
	}
	p.SetPolicy(policy)
	conn, err := net.Dial("tcp", p.Addr())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	target := closedAddress(t)
	if _, err := io.WriteString(conn, "CONNECT "+target+" HTTP/1.1\r\nHost: "+target+"\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway || string(body) != "Bad Gateway\n" {
		t.Fatalf("status %d body %q", resp.StatusCode, body)
	}
	if created, updated := sink.counts(); created != 0 || updated != 0 {
		t.Fatalf("sink %d %d", created, updated)
	}
}

func TestTunnelRejectsSelf(t *testing.T) {
	sink := &memSink{}
	p := startProxy(t, sink, 1024, nil)
	policy, err := capture.Compile(false, "all", "tunnel", nil)
	if err != nil {
		t.Fatal(err)
	}
	p.SetPolicy(policy)
	conn, err := net.Dial("tcp", p.Addr())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	target := p.Addr()
	if _, err := io.WriteString(conn, "CONNECT "+target+" HTTP/1.1\r\nHost: "+target+"\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway || string(body) != "refusing to proxy to the proxy's own listen address\n" {
		t.Fatalf("status %d body %q", resp.StatusCode, body)
	}
}

func TestPlainKeepAliveUsesTheNewPolicy(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	defer upstream.Close()
	sink := &memSink{}
	p := startProxy(t, sink, 1024, nil)
	conn, err := net.Dial("tcp", p.Addr())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	reader := bufio.NewReader(conn)
	send := func() {
		t.Helper()
		if _, err := io.WriteString(conn, "GET "+upstream.URL+" HTTP/1.1\r\nHost: upstream\r\n\r\n"); err != nil {
			t.Fatal(err)
		}
		resp, err := http.ReadResponse(reader, nil)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status %d", resp.StatusCode)
		}
	}
	send()
	// Recording is asynchronous: OnUpdated lands after the response is written.
	waitFlowCount(t, sink, 1)
	if created, updated := sink.counts(); created != 1 || updated != 1 {
		t.Fatalf("first sink %d %d", created, updated)
	}
	policy, err := capture.Compile(false, "all", "decrypt", nil)
	if err != nil {
		t.Fatal(err)
	}
	p.SetPolicy(policy)
	send()
	waitForRecords(t, p)
	if created, updated := sink.counts(); created != 1 || updated != 1 {
		t.Fatalf("second sink %d %d", created, updated)
	}
}

func TestTLSFailureIsSilentWhenNotRecording(t *testing.T) {
	manager, _ := newCA(t)
	sink := &memSink{}
	p := startProxy(t, sink, 1024, manager)
	policy, err := capture.Compile(false, "all", "decrypt", nil)
	if err != nil {
		t.Fatal(err)
	}
	p.SetPolicy(policy)
	_, err = dialMITM(t, p.Addr(), "untrusted.example:443", &tls.Config{
		RootCAs:    x509.NewCertPool(),
		ServerName: "untrusted.example",
	})
	if err == nil {
		t.Fatal("client handshake unexpectedly succeeded")
	}
	waitForRecords(t, p)
	if created, updated := sink.counts(); created != 0 || updated != 0 {
		t.Fatalf("sink %d %d", created, updated)
	}
}

func TestConnectKeepsItsDecisionAfterSetPolicy(t *testing.T) {
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "still-recorded")
	}))
	defer upstream.Close()
	manager, pool := newCA(t)
	sink := &memSink{}
	p := startProxy(t, sink, 1024, manager)
	target := upstream.Listener.Addr().String()
	host, _, _ := net.SplitHostPort(target)
	tlsConn, err := dialMITM(t, p.Addr(), target, &tls.Config{
		RootCAs:    pool,
		ServerName: host,
		NextProtos: []string{"http/1.1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	policy, err := capture.Compile(false, "all", "decrypt", nil)
	if err != nil {
		t.Fatal(err)
	}
	p.SetPolicy(policy)
	if _, err := io.WriteString(tlsConn, "GET / HTTP/1.1\r\nHost: "+target+"\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	resp, err := http.ReadResponse(bufio.NewReader(tlsConn), &http.Request{Method: http.MethodGet})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(body) != "still-recorded" {
		t.Fatalf("status %d body %q", resp.StatusCode, body)
	}
	f := waitFlowCount(t, sink, 1)
	if f.Scheme != "https" || f.StatusCode != http.StatusOK {
		t.Fatalf("flow %+v", f)
	}
}

func TestDecryptOnlyHTTPSForwardsWithoutCapture(t *testing.T) {
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "clear-body")
	}))
	defer upstream.Close()
	manager, pool := newCA(t)
	sink := &memSink{}
	p := startProxy(t, sink, 1024, manager)
	policy, err := capture.Compile(true, "none", "decrypt", nil)
	if err != nil {
		t.Fatal(err)
	}
	p.SetPolicy(policy)
	var captures atomic.Int64
	p.bodyCaptureHook = func() { captures.Add(1) }
	target := upstream.Listener.Addr().String()
	host, _, _ := net.SplitHostPort(target)
	tlsConn, err := dialMITM(t, p.Addr(), target, &tls.Config{
		RootCAs:    pool,
		ServerName: host,
		NextProtos: []string{"http/1.1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(tlsConn, "GET / HTTP/1.1\r\nHost: "+target+"\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	resp, err := http.ReadResponse(bufio.NewReader(tlsConn), &http.Request{Method: http.MethodGet})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(body) != "clear-body" {
		t.Fatalf("status %d body %q", resp.StatusCode, body)
	}
	waitForRecords(t, p)
	if created, updated := sink.counts(); created != 0 || updated != 0 {
		t.Fatalf("sink created=%d updated=%d", created, updated)
	}
	if captures.Load() != 0 {
		t.Fatal("decrypt-only allocated a body capture")
	}
}

func waitForRecords(t *testing.T, p *Proxy) {
	t.Helper()
	done := make(chan struct{})
	p.mu.Lock()
	jobs := p.jobs
	started := p.recStarted
	p.mu.Unlock()
	if !started || jobs == nil {
		t.Fatal("recorder is not running")
	}
	select {
	case jobs <- recordJob{flush: true, done: done}:
	case <-time.After(3 * time.Second):
		t.Fatal("recorder queue is full")
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("recorder did not finish")
	}
}

func waitFlowCount(t *testing.T, sink *memSink, n int) flow.Flow {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, updated := sink.counts(); updated >= n {
			f, _ := sink.last()
			return f
		}
		time.Sleep(time.Millisecond)
	}
	_, updated := sink.counts()
	t.Fatalf("got %d updated flows, want %d", updated, n)
	return flow.Flow{}
}
