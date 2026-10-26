# HTTPS Warm-Path Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Keep the extra time of a repeat HTTPS request to the same host within 10 ms of a direct connection, while still recording headers and bodies up to `max_body`.

**Architecture:** One shared `http.Transport` keeps 6 idle conns per host and 128 overall, with a 128-entry TLS session cache. CONNECT-to-IP with a different SNI uses a 32-entry LRU of transports that share that cache. A single recorder goroutine runs `OnCreated` / decode / `OnUpdated` off the connection goroutine; a full queue records that new flow inline, and a flow that already queued its begin waits for a slot to queue its end.

**Tech Stack:** Go 1.24.4, standard `net/http` and `crypto/tls`. No new module dependency.

**Spec:** `docs/superpowers/specs/2026-10-26-https-warm-path-design.md`

## Global Constraints

- Repeat requests to one host must stay within 10 ms of a direct 1 KB localhost TLS round trip, measured as the median of 20 proxy samples minus the median of 20 direct samples. `BenchmarkWarmHTTPS` prints that number and never fails `go test`.
- Still MITM every HTTPS request. Record headers, both bodies up to the flow's starting `max_body`, raw sizes, truncation flags, and a decoded copy when `gzip`, `br`, or `deflate` decodes. Do not add a passthrough mode.
- Do not speed up first-use leaf issuance, and do not share one leaf private key across hosts.
- Do not change `FlowSink`, the store, WebSocket messages, or the frontend.
- Keep `DisableCompression: true`, ALPN `h2` then `http/1.1`, and `ExpectContinueTimeout` at 1 second. Do not add `ResponseHeaderTimeout`. Leave client-facing session tickets at the Go default.
- Main transport: `MaxIdleConnsPerHost` 6, `MaxIdleConns` 128, `IdleConnTimeout` 90 seconds, dial timeout 30 seconds, TCP keepalive 30 seconds, TLS handshake timeout 10 seconds. The value 6 must be written explicitly. Go treats 0 as the default 2.
- Session cache capacity 128, LRU, shared by the main transport and every SNI transport. `upstreamDisableKeepAlive` defaults false. Tests may set it true before the first dial.
- SNI cache key is `req.URL.Host` plus the SNI hostname, cap 32, most recently used kept. Each entry has `MaxIdleConns` 6 and `MaxIdleConnsPerHost` 6. Eviction calls `CloseIdleConnections` only. Do not hold the lookup lock across `RoundTrip`.
- Recorder queue default 64, one goroutine. `New` stores 64. The goroutine starts on `ListenAndServe` or the first `beginRecord`, whichever comes first, using `recordQueue` at that moment. A nil `recordGate` does not block. A non-nil gate is received from before every take.
- Begin enqueue is non-blocking. Failure records that whole flow inline: `OnCreated` before `RoundTrip`, `OnUpdated` after the write. End enqueue of an accepted flow blocks until a slot exists. Never inline that end.
- `finishFlow` runs on the connection before decode. `DurationMs` does not include decode or the sink call. Close the upstream body, then record.
- Upstream failure writes `502` and then records. Keep only the request bytes already read. No response decode. A short client write drains the rest through the existing tee, keeps the write error string, and still tries decode. Decode failure leaves the raw body and does not append to `error`.
- TLS handshake EOF records nothing. Every other client handshake failure records a CONNECT flow whose `Error` is exactly `TLS handshake with client failed: ` plus `err.Error()`.
- WebSocket `101` is recorded through the same handoff. Duration stops at the upgrade. Copying the tunnel does not update the flow again. Keep the current duplex and write-error strings.
- `Close` marks closed, closes the listener, client conns, and idle conns on the main and SNI transports, waits for handler goroutines, then lets the recorder drain and exit. `Close` waits for an in-flight `OnUpdated`. It does not cancel the sink call.
- `git add` only the paths named in the task. Never `git add -A`.

## File Structure

| Path | Responsibility |
| --- | --- |
| `internal/proxy/proxy.go` | Transport builder, session cache, SNI LRU, `Close` order, handler wait group |
| `internal/proxy/record.go` | Queue, gate, inline begin, blocking end, decode-then-`OnUpdated` |
| `internal/proxy/http1.go` | Cleartext, CONNECT, MITM HTTP/1, WebSocket, TLS failure handoff |
| `internal/proxy/http2.go` | MITM HTTP/2 handoff |
| `internal/proxy/warm_test.go` | Reuse, resumption, SNI cache, recorder order |
| `internal/proxy/limit_test.go` | Close the proxy and read the body from the sink |
| `internal/proxy/warm_bench_test.go` | `BenchmarkWarmHTTPS` |

---

### Task 1: Keep six idle HTTP/1 connections per host

**Files:**
- Modify: `internal/proxy/proxy.go`
- Test: `internal/proxy/warm_test.go`

**Interfaces:**
- Consumes: `Proxy.upstream()`, `Proxy.UpstreamTLS`, `startProxy`, `newCA`, `waitAddr`
- Produces:
  - `func (p *Proxy) buildTransport(base *tls.Config, maxIdle, perHost int) *http.Transport`
  - `func (p *Proxy) baseTLS() *tls.Config`
  - constants `maxIdleConnsTotal = 128`, `maxIdleConnsPerHost = 6`
  - `upstream()` uses `buildTransport(p.baseTLS(), maxIdleConnsTotal, maxIdleConnsPerHost)` and still publishes one shared `*http.Transport`

- [ ] **Step 1: Write the failing test**

Create `internal/proxy/warm_test.go`:

```go
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
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/proxy/ -run TestIdleConnsReuseSixHTTP1 -count=1`

Expected: FAIL because `second wave accepts` is greater than 6 (the default per-host idle pool is 2, so the second wave opens about 4 new conns).

- [ ] **Step 3: Build the shared transport with explicit idle limits**

In `internal/proxy/proxy.go`, add the constants next to the existing proxy types:

```go
const (
	maxIdleConnsTotal   = 128
	maxIdleConnsPerHost = 6
)
```

Replace `upstream` with a builder. `buildTransport` must set `MaxIdleConns` and `MaxIdleConnsPerHost` from its arguments. `ForceAttemptHTTP2`, `DisableCompression`, the 30 second dialer, 90 second idle timeout, 10 second handshake timeout, and 1 second `ExpectContinueTimeout` stay as they are today.

```go
func (p *Proxy) baseTLS() *tls.Config {
	if p.UpstreamTLS != nil {
		return p.UpstreamTLS.Clone()
	}
	return &tls.Config{}
}

func (p *Proxy) buildTransport(base *tls.Config, maxIdle, perHost int) *http.Transport {
	if base == nil {
		base = &tls.Config{}
	}
	cfg := base.Clone()
	cfg.NextProtos = []string{"h2", "http/1.1"}
	return &http.Transport{
		DialContext:           (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSClientConfig:       cfg,
		ForceAttemptHTTP2:     true,
		DisableCompression:    true,
		MaxIdleConns:          maxIdle,
		MaxIdleConnsPerHost:   perHost,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
}

func (p *Proxy) upstream() *http.Transport {
	p.mu.Lock()
	tr := p.transport
	base := p.baseTLS()
	p.mu.Unlock()
	if tr != nil {
		return tr
	}
	built := p.buildTransport(base, maxIdleConnsTotal, maxIdleConnsPerHost)
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.transport == nil {
		p.transport = built
		return built
	}
	return p.transport
}
```

`baseTLS` reads `UpstreamTLS`. Call it while deciding to build, before `buildTransport`. Do not call `baseTLS` twice under the assumption that `UpstreamTLS` changes later. Tests set `UpstreamTLS` before the first request.

Keep `roundTrip`'s SNI branch on its own transport for this task. Task 3 replaces that branch.

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/proxy/ -run TestIdleConnsReuseSixHTTP1 -count=1`

Expected: PASS.

- [ ] **Step 5: Lock one-connection reuse for repeated requests**

Append to `internal/proxy/warm_test.go`. These two tests pass on the current tree as well as after Step 3. They lock the spec's "five sequential requests, one upstream accept" cases for HTTP/1.1 and HTTP/2.

```go
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
```

Add `"golang.org/x/net/http2"` to the imports.

Run: `go test ./internal/proxy/ -run 'TestIdleConnsReuseSixHTTP1|TestSequentialHTTP1UsesOneUpstream|TestSequentialHTTP2UsesOneUpstream' -count=1`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/proxy/proxy.go internal/proxy/warm_test.go
git commit -m "$(cat <<'EOF'
feat: keep six idle connections per upstream host

EOF
)"
```

---

### Task 2: Resume upstream TLS sessions

**Files:**
- Modify: `internal/proxy/proxy.go`
- Modify: `internal/proxy/warm_test.go`

**Interfaces:**
- Consumes: `buildTransport`, `baseTLS`, `http1TLSServer`, `trustClient`, `getOK`, `startProxy`, `newCA`
- Produces:
  - `Proxy.upstreamDisableKeepAlive bool`
  - `Proxy.sessions tls.ClientSessionCache`
  - `func (p *Proxy) sessionCache() tls.ClientSessionCache` returning one `tls.NewLRUClientSessionCache(128)` for the process
  - `buildTransport` sets `TLSClientConfig.ClientSessionCache` to `p.sessionCache()` and `DisableKeepAlives` to `p.upstreamDisableKeepAlive`

- [ ] **Step 1: Write the failing test**

Append to `internal/proxy/warm_test.go`:

```go
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
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/proxy/ -run TestUpstreamTLSResumesWhenKeepAliveIsOff -count=1`

Expected: FAIL with `DidResume = [false false]`.

- [ ] **Step 3: Share one session cache and honor the keep-alive switch**

Add the field next to `transport` in `Proxy`:

```go
upstreamDisableKeepAlive bool
sessions                 tls.ClientSessionCache
```

```go
const sessionCacheEntries = 128

func (p *Proxy) sessionCache() tls.ClientSessionCache {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.sessions == nil {
		p.sessions = tls.NewLRUClientSessionCache(sessionCacheEntries)
	}
	return p.sessions
}
```

`buildTransport` currently locks nothing. It must read `upstreamDisableKeepAlive` without nesting `p.mu` inside `sessionCache`. Read the flag first, then call `sessionCache`:

```go
func (p *Proxy) buildTransport(base *tls.Config, maxIdle, perHost int) *http.Transport {
	if base == nil {
		base = &tls.Config{}
	}
	p.mu.Lock()
	disableKeepAlive := p.upstreamDisableKeepAlive
	p.mu.Unlock()
	cfg := base.Clone()
	cfg.NextProtos = []string{"h2", "http/1.1"}
	cfg.ClientSessionCache = p.sessionCache()
	return &http.Transport{
		DialContext:           (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSClientConfig:       cfg,
		ForceAttemptHTTP2:     true,
		DisableCompression:    true,
		DisableKeepAlives:     disableKeepAlive,
		MaxIdleConns:          maxIdle,
		MaxIdleConnsPerHost:   perHost,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
}
```

`upstream()` calls `baseTLS()` while it may hold no lock across `buildTransport`. `buildTransport` takes `p.mu` itself. Do not call `buildTransport` with `p.mu` already held.

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/proxy/ -run 'TestIdleConnsReuseSixHTTP1|TestUpstreamTLSResumesWhenKeepAliveIsOff' -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/proxy/proxy.go internal/proxy/warm_test.go
git commit -m "$(cat <<'EOF'
feat: resume upstream TLS sessions

EOF
)"
```

---

### Task 3: Reuse the upstream when CONNECT dials an IP

**Files:**
- Modify: `internal/proxy/proxy.go`
- Modify: `internal/proxy/warm_test.go`

**Interfaces:**
- Consumes: `buildTransport`, `baseTLS`, `hostWithoutPort`, `roundTrip`
- Produces:
  - `type sniTransport struct { key string; tr *http.Transport }`
  - `Proxy.sniTransports []sniTransport` with the most recently used entry at the end
  - `func (p *Proxy) sniTransport(dialHost, sniHost string) *http.Transport`
  - `roundTrip` uses `sniTransport(req.URL.Host, hostWithoutPort(upstreamSNI))` when the SNI host differs from the dial host
  - `Close` calls `CloseIdleConnections` on every cached SNI transport as well as `p.transport`

- [ ] **Step 1: Write the failing test**

Append to `internal/proxy/warm_test.go`:

```go
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
```

Add `"bufio"` to the imports.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/proxy/ -run TestSNIMismatchReusesUpstream -count=1`

Expected: FAIL with `upstream accepts = 2, want 1`.

- [ ] **Step 3: Cache one transport per dial address and SNI**

Add the cache and a constant:

```go
const maxSNITransports = 32

type sniTransport struct {
	key string
	tr  *http.Transport
}
```

Add `sniTransports []sniTransport` to `Proxy`.

```go
func (p *Proxy) touchSNI(key string) *http.Transport {
	for i := range p.sniTransports {
		if p.sniTransports[i].key != key {
			continue
		}
		entry := p.sniTransports[i]
		copy(p.sniTransports[i:], p.sniTransports[i+1:])
		p.sniTransports[len(p.sniTransports)-1] = entry
		return entry.tr
	}
	return nil
}

func (p *Proxy) sniTransport(dialHost, sniHost string) *http.Transport {
	key := dialHost + "\n" + sniHost
	p.mu.Lock()
	if tr := p.touchSNI(key); tr != nil {
		p.mu.Unlock()
		return tr
	}
	p.mu.Unlock()

	base := p.baseTLS()
	base.ServerName = sniHost
	tr := p.buildTransport(base, maxIdleConnsPerHost, maxIdleConnsPerHost)
	tr.DialTLSContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		d := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
		raw, err := d.DialContext(ctx, network, dialHost)
		if err != nil {
			return nil, err
		}
		tc := tls.Client(raw, tr.TLSClientConfig)
		if err := tc.HandshakeContext(ctx); err != nil {
			_ = raw.Close()
			return nil, err
		}
		return tc, nil
	}

	p.mu.Lock()
	if existing := p.touchSNI(key); existing != nil {
		p.mu.Unlock()
		tr.CloseIdleConnections()
		return existing
	}
	var evict *http.Transport
	if len(p.sniTransports) == maxSNITransports {
		evict = p.sniTransports[0].tr
		p.sniTransports = p.sniTransports[1:]
	}
	p.sniTransports = append(p.sniTransports, sniTransport{key: key, tr: tr})
	p.mu.Unlock()
	if evict != nil {
		evict.CloseIdleConnections()
	}
	return tr
}
```

`touchSNI` is called only while `p.mu` is held. `CloseIdleConnections` runs after the lock is released. A concurrent caller that built a duplicate finds the inserted entry on the second lookup and closes its own idle conns.

Replace the SNI branch of `roundTrip` so it does not build a transport inline:

```go
func (p *Proxy) roundTrip(req *http.Request, upstreamSNI string) (*http.Response, error) {
	if upstreamSNI == "" || !strings.EqualFold(req.URL.Scheme, "https") {
		return p.upstream().RoundTrip(req)
	}
	dialHost := req.URL.Host
	sniHost := hostWithoutPort(upstreamSNI)
	if sniHost == "" || strings.EqualFold(sniHost, hostWithoutPort(dialHost)) {
		return p.upstream().RoundTrip(req)
	}
	return p.sniTransport(dialHost, sniHost).RoundTrip(req)
}
```

Delete the old per-call `http.Transport` literal in `roundTrip`.

In `Close`, after `transport.CloseIdleConnections()`, copy `p.sniTransports` under `p.mu` and call `CloseIdleConnections` on each. Do not hold `p.mu` across those calls. Take the copy in the same critical section that already reads `p.transport`.

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/proxy/ -run 'TestIdleConnsReuseSixHTTP1|TestUpstreamTLSResumesWhenKeepAliveIsOff|TestSNIMismatchReusesUpstream' -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/proxy/proxy.go internal/proxy/warm_test.go
git commit -m "$(cat <<'EOF'
feat: reuse upstream connections for IP CONNECT targets

EOF
)"
```

---

### Task 4: Record off the forward path

**Files:**
- Create: `internal/proxy/record.go`
- Modify: `internal/proxy/proxy.go`
- Modify: `internal/proxy/http1.go`
- Modify: `internal/proxy/http2.go`
- Modify: `internal/proxy/limit_test.go`
- Modify: `internal/proxy/warm_test.go`

**Interfaces:**
- Consumes: `FlowSink`, `bodyCapture.result`, `decodeForDisplay`, `finishFlow`, `copyHeaders`, `streamRequestBody`
- Produces:
  - `type flowRecord struct { inline bool; f *flow.Flow; reqCap *bodyCapture; limit int64; reqLen int64 }`
  - `type recordJob struct { begin bool; f flow.Flow; reqCap *bodyCapture; respCap *bodyCapture; respHeader http.Header; limit int64; reqLen int64 }`
  - `func (p *Proxy) ensureRecorder()`
  - `func (p *Proxy) stopRecorder()`
  - `func (p *Proxy) beginRecord(f *flow.Flow, reqCap *bodyCapture, limit, reqLen int64) *flowRecord`
  - `func (p *Proxy) finishRecord(rec *flowRecord, respHeader http.Header, respCap *bodyCapture)`
  - `func (p *Proxy) startFlow(req *http.Request, f *flow.Flow) (*flowRecord, int64)`
  - `Proxy.recordQueue int`, `Proxy.recordGate chan struct{}`, `Proxy.jobs chan recordJob`, `Proxy.handlerWG sync.WaitGroup`
  - `failUpstream(rec *flowRecord, err error)` sets status 502 and `err.Error()`, then `finishRecord(rec, nil, nil)`

- [ ] **Step 1: Write the failing tests**

Append to `internal/proxy/warm_test.go`:

```go
type gateSink struct {
	mu          sync.Mutex
	order       []string
	updated     []flow.Flow
	entered     chan struct{}
	release     chan struct{}
	once        sync.Once
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
```

Add `"time"` to the imports in `warm_test.go`. `gateSink.OnUpdated` blocks only its first call. The full-queue test closes `release` before any request, so that inline `OnUpdated` does not block. `/first` signals from the upstream handler, which is after its begin job has been queued. `/second` must be fully updated before `recordGate` is closed.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/proxy/ -run 'TestSecondRequestReturnsWhileUpdateIsBlocked|TestFullQueueRecordsTheNewFlowInline' -count=1`

Expected: `TestSecondRequestReturnsWhileUpdateIsBlocked` fails with `second request waited for OnUpdated`. The queue test fails because `recordQueue` and `recordGate` do not exist yet, or because `/second` is not updated before the gate opens.

- [ ] **Step 3: Add the recorder**

Create `internal/proxy/record.go`:

```go
package proxy

import (
	"net/http"
	"sync"

	"httpproxy/internal/flow"
)

const defaultRecordQueue = 64

type flowRecord struct {
	inline bool
	f      *flow.Flow
	reqCap *bodyCapture
	limit  int64
	reqLen int64
}

type recordJob struct {
	begin      bool
	f          flow.Flow
	reqCap     *bodyCapture
	respCap    *bodyCapture
	respHeader http.Header
	limit      int64
	reqLen     int64
}

func (p *Proxy) ensureRecorder() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.recStarted {
		return
	}
	n := p.recordQueue
	if n < 1 {
		n = defaultRecordQueue
	}
	p.jobs = make(chan recordJob, n)
	p.recStarted = true
	p.recWG.Add(1)
	go p.recordLoop()
}

func (p *Proxy) waitRecordGate() {
	p.mu.Lock()
	gate := p.recordGate
	p.mu.Unlock()
	if gate != nil {
		<-gate
	}
}

func (p *Proxy) recordLoop() {
	defer p.recWG.Done()
	for {
		p.waitRecordGate()
		job, ok := <-p.jobs
		if !ok {
			return
		}
		p.applyJob(job)
	}
}

func (p *Proxy) stopRecorder() {
	p.recStop.Do(func() {
		p.mu.Lock()
		started := p.recStarted
		jobs := p.jobs
		p.mu.Unlock()
		if !started {
			return
		}
		close(jobs)
		p.recWG.Wait()
	})
}

func (p *Proxy) beginRecord(f *flow.Flow, reqCap *bodyCapture, limit, reqLen int64) *flowRecord {
	p.ensureRecorder()
	rec := &flowRecord{f: f, reqCap: reqCap, limit: limit, reqLen: reqLen}
	snap := *f
	snap.ReqBody = nil
	snap.RespBody = nil
	snap.RespBodyDecoded = nil
	select {
	case p.jobs <- recordJob{begin: true, f: snap}:
		return rec
	default:
		rec.inline = true
		p.created(snap)
		return rec
	}
}

func (p *Proxy) finishRecord(rec *flowRecord, respHeader http.Header, respCap *bodyCapture) {
	finishFlow(rec.f, rec.f.StartedAt)
	job := recordJob{
		f:          *rec.f,
		reqCap:     rec.reqCap,
		respCap:    respCap,
		respHeader: respHeader,
		limit:      rec.limit,
		reqLen:     rec.reqLen,
	}
	job.f.ReqBody = nil
	job.f.RespBody = nil
	job.f.RespBodyDecoded = nil
	if rec.inline {
		p.applyJob(job)
		return
	}
	p.jobs <- job
}

func (p *Proxy) applyJob(job recordJob) {
	if job.begin {
		p.created(job.f)
		return
	}
	if job.reqCap != nil {
		job.f.ReqBody, job.f.ReqSize, job.f.ReqTruncated = job.reqCap.result()
		if job.reqLen > job.f.ReqSize {
			job.f.ReqSize = job.reqLen
			job.f.ReqTruncated = job.f.ReqSize > job.limit
		}
	}
	if job.respCap != nil {
		job.f.RespBody, job.f.RespSize, job.f.RespTruncated = job.respCap.result()
		job.f.RespBodyDecoded = decodeForDisplay(job.f.RespBody, job.respHeader, job.limit)
	}
	p.updated(job.f)
}
```

Add these fields to `Proxy`:

```go
recordQueue int
recordGate  chan struct{}
jobs        chan recordJob
recStarted  bool
recStop     sync.Once
recWG       sync.WaitGroup
handlerWG   sync.WaitGroup
```

`New` sets `recordQueue: defaultRecordQueue` only. It does not start the goroutine.

`ListenAndServe` calls `p.ensureRecorder()` after the listener is stored and before `Accept`.

In the accept loop, increment `handlerWG` under the same `p.mu` section that inserts `conn` into `p.conns`, and `defer p.handlerWG.Done()` in that goroutine. If `p.closed` is already set, do not `Add`.

`Close` keeps its current listener, conn, and main-transport shutdown. After closing idle conns on the main transport and every SNI transport, call `p.handlerWG.Wait()` and then `p.stopRecorder()`. The early "already closed" return stays before that wait, so a second `Close` does not wait again. `stopRecorder` is protected by `recStop`.

- [ ] **Step 4: Hand every flow to the recorder**

Replace `startFlow`, `failUpstream`, and delete `finishResponse` / `setRequestCapture` once the callers below no longer use them.

```go
func (p *Proxy) startFlow(req *http.Request, f *flow.Flow) (*flowRecord, int64) {
	limit := p.maxBodyLimit()
	f.ReqHeaders = copyHeaders(req.Header)
	reqCap := streamRequestBody(req, limit)
	return p.beginRecord(f, reqCap, limit, req.ContentLength), limit
}

func (p *Proxy) failUpstream(rec *flowRecord, err error) {
	rec.f.StatusCode = http.StatusBadGateway
	rec.f.Error = err.Error()
	p.finishRecord(rec, nil, nil)
}
```

Change the accept-loop registration to:

```go
p.mu.Lock()
if p.closed {
    p.mu.Unlock()
    _ = conn.Close()
    return nil
}
p.conns[conn] = struct{}{}
p.handlerWG.Add(1)
p.mu.Unlock()

go func() {
    defer p.handlerWG.Done()
    defer func() {
        _ = conn.Close()
        p.mu.Lock()
        delete(p.conns, conn)
        p.mu.Unlock()
    }()
    p.handleHTTP1(conn)
}()
```

Replace `Close` with:

```go
func (p *Proxy) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	listener := p.listener
	conns := make([]net.Conn, 0, len(p.conns))
	for conn := range p.conns {
		conns = append(conns, conn)
	}
	transport := p.transport
	sni := append([]sniTransport(nil), p.sniTransports...)
	p.mu.Unlock()

	var closeErr error
	if listener != nil {
		closeErr = listener.Close()
	}
	for _, conn := range conns {
		_ = conn.Close()
	}
	if transport != nil {
		transport.CloseIdleConnections()
	}
	for _, entry := range sni {
		entry.tr.CloseIdleConnections()
	}
	p.handlerWG.Wait()
	p.stopRecorder()
	return closeErr
}
```

In `handleRequest`, keep the setup through the `flow.Flow` literal. From `startFlow` to the return, use:

```go
	rec, limit := p.startFlow(req, &f)
	if defaultHost != "" {
		req.URL.Scheme = scheme
		req.URL.Host = defaultHost
	} else if req.URL.Scheme == "" {
		req.URL.Scheme = scheme
		if req.URL.Host == "" {
			req.URL.Host = host
		}
	}
	if p.isLoopbackTarget(req.URL) {
    writeResponse(conn, http.StatusBadGateway, "Bad Gateway\n")
    p.failUpstream(rec, errSelfProxy)
    return false
}
prepareOutgoing(req, websocket)
resp, err := p.roundTrip(req, upstreamSNI)
if err != nil {
    writeResponse(conn, http.StatusBadGateway, "Bad Gateway\n")
    p.failUpstream(rec, err)
    return false
}
defer func() { _ = resp.Body.Close() }()
f.StatusCode = resp.StatusCode
f.RespHeaders = copyHeaders(resp.Header)
if websocket && resp.StatusCode == http.StatusSwitchingProtocols {
    return p.tunnelWebSocket(conn, req, resp, &f, rec)
}
respCapture := newBodyCapture(limit)
resp.Body = struct {
    io.Reader
    io.Closer
}{Reader: io.TeeReader(resp.Body, respCapture), Closer: resp.Body}
closeAfter := normalizeForHTTP1Client(resp, req, clientClose)
writeErr := resp.Write(conn)
if writeErr != nil {
    _, _ = io.Copy(io.Discard, resp.Body)
    f.Error = writeErr.Error()
}
_ = resp.Body.Close()
resp.Body = http.NoBody
p.finishRecord(rec, resp.Header, respCapture)
return writeErr == nil && !closeAfter
```

`io.Copy(io.Discard, resp.Body)` stays. That reader is already the `TeeReader`, so the unread tail is captured once.

```go
func (p *Proxy) tunnelWebSocket(conn net.Conn, req *http.Request, resp *http.Response, f *flow.Flow, rec *flowRecord) bool {
    removeHopHeadersPreserveUpgrade(resp.Header)
    resp.Proto = "HTTP/1.1"
    resp.ProtoMajor = 1
    resp.ProtoMinor = 1
    resp.Close = false
    upstreamConn, ok := resp.Body.(io.ReadWriteCloser)
    if !ok {
        _ = resp.Body.Close()
        resp.Body = http.NoBody
        f.Error = "websocket upgrade: upstream body is not duplex"
        p.finishRecord(rec, nil, nil)
        writeResponse(conn, http.StatusBadGateway, "Bad Gateway\n")
        return false
    }
    resp.Body = http.NoBody
    if err := resp.Write(conn); err != nil {
        _ = upstreamConn.Close()
        f.Error = err.Error()
        p.finishRecord(rec, nil, nil)
        return false
    }
    p.finishRecord(rec, resp.Header, nil)
    errCh := make(chan error, 2)
    go func() {
        _, err := io.Copy(upstreamConn, conn)
        errCh <- err
    }()
    go func() {
        _, err := io.Copy(conn, upstreamConn)
        errCh <- err
    }()
    <-errCh
    _ = upstreamConn.Close()
    return false
}
```

Do not record again when the tunnel copies finish. Leave the `errors.Is(err, io.EOF)` guard in `handleConnect` unchanged.

```go
func (p *Proxy) recordTLSFailure(conn net.Conn, authority string, started time.Time, err error) {
    f := flow.Flow{
        ID:          uuid.NewString(),
        Scheme:      "https",
        Method:      http.MethodConnect,
        Host:        authority,
        HTTPVersion: "1.1",
        StartedAt:   started,
        ClientAddr:  conn.RemoteAddr().String(),
        Error:       "TLS handshake with client failed: " + err.Error(),
    }
    rec := p.beginRecord(&f, nil, 0, 0)
    p.finishRecord(rec, nil, nil)
}
```

In `handleHTTP2Request`, keep the setup through the `flow.Flow` literal. On a write error, `io.Copy(respCapture, resp.Body)` still reads the raw body, not the tee. From `startFlow` to the end:

```go
	rec, limit := p.startFlow(req, &f)
req.URL.Scheme = "https"
req.URL.Host = connectAuthority
if p.isLoopbackTarget(req.URL) {
    http.Error(w, "Bad Gateway", http.StatusBadGateway)
    p.failUpstream(rec, errSelfProxy)
    return
}
prepareOutgoing(req, false)
resp, err := p.roundTrip(req, upstreamSNI)
if err != nil {
    http.Error(w, "Bad Gateway", http.StatusBadGateway)
    p.failUpstream(rec, err)
    return
}
defer func() { _ = resp.Body.Close() }()
f.StatusCode = resp.StatusCode
f.RespHeaders = copyHeaders(resp.Header)
removeHopHeaders(resp.Header)
for name, values := range resp.Header {
    for _, value := range values {
        w.Header().Add(name, value)
    }
}
w.WriteHeader(resp.StatusCode)
respCapture := newBodyCapture(limit)
writeErr := copyAndFlush(w, io.TeeReader(resp.Body, respCapture))
if writeErr != nil {
    _, _ = io.Copy(respCapture, resp.Body)
    f.Error = writeErr.Error()
}
_ = resp.Body.Close()
resp.Body = http.NoBody
p.finishRecord(rec, resp.Header, respCapture)
```

Replace `TestFlowKeepsStartLimit` with a sink read. The second `startFlow` still only checks its limit.

```go
func TestFlowKeepsStartLimit(t *testing.T) {
    sink := &memSink{}
    p := New("127.0.0.1:0", sink, 4, nil)
    t.Cleanup(func() { _ = p.Close() })

    req := httptest.NewRequest(http.MethodPost, "http://example.com/", strings.NewReader("abcdefghij"))
    var first flow.Flow
    rec, limit := p.startFlow(req, &first)
    if limit != 4 {
        t.Fatalf("start limit = %d, want 4", limit)
    }
    if _, err := io.ReadAll(req.Body); err != nil {
        t.Fatal(err)
    }
    stored, _, truncated := rec.reqCap.result()
    if string(stored) != "abcd" || !truncated {
        t.Fatalf("request capture = %q truncated=%t, want %q truncated", stored, truncated, "abcd")
    }

    p.SetMaxBody(2)
    req2 := httptest.NewRequest(http.MethodPost, "http://example.com/", strings.NewReader("abcdefghij"))
    var second flow.Flow
    _, limit2 := p.startFlow(req2, &second)
    if limit2 != 2 {
        t.Fatalf("second start limit = %d, want 2", limit2)
    }

    const respBody = "12345678"
    respCapture := newBodyCapture(limit)
    if _, err := io.Copy(respCapture, strings.NewReader(respBody)); err != nil {
        t.Fatal(err)
    }
    resp := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header)}
    p.finishRecord(rec, resp.Header, respCapture)
    got := waitUpdatedFlow(t, sink)
    if string(got.RespBody) != "1234" {
        t.Fatalf("stored response prefix = %q, want 4 bytes %q", got.RespBody, "1234")
    }
}
```

- [ ] **Step 5: Run the proxy tests**

Run: `go test ./internal/proxy/ -count=1`

Expected: PASS, including the two new recorder tests and the existing capture, gzip, HTTP/2, WebSocket, TLS-failure, and self-proxy tests.

- [ ] **Step 6: Commit**

```bash
git add internal/proxy/record.go internal/proxy/proxy.go internal/proxy/http1.go internal/proxy/http2.go internal/proxy/limit_test.go internal/proxy/warm_test.go
git commit -m "$(cat <<'EOF'
feat: record flows off the forward path

EOF
)"
```

---

### Task 5: Benchmark the warm HTTPS round trip

**Files:**
- Create: `internal/proxy/warm_bench_test.go`

**Interfaces:**
- Consumes: `Proxy.New`, `cert.LoadOrCreate`, `wait` loop on `Proxy.Addr`. No new production API.
- Produces: `func BenchmarkWarmHTTPS(b *testing.B)` and `func medianDuration(samples []time.Duration) time.Duration`

- [ ] **Step 1: Write the benchmark**

Create `internal/proxy/warm_bench_test.go`:

```go
package proxy

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"testing"
	"time"

	"httpproxy/internal/cert"
)

func medianDuration(samples []time.Duration) time.Duration {
	ordered := append([]time.Duration(nil), samples...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	return ordered[len(ordered)/2]
}

func BenchmarkWarmHTTPS(b *testing.B) {
	body := bytes.Repeat([]byte("a"), 1024)
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}))
	b.Cleanup(upstream.Close)
	manager, err := cert.LoadOrCreate(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(manager.CACertPEM()) {
		b.Fatal("failed to trust proxy CA")
	}
	p := New("127.0.0.1:0", &memSink{}, 2048, manager)
	p.UpstreamTLS = &tls.Config{InsecureSkipVerify: true}
	errCh := make(chan error, 1)
	go func() { errCh <- p.ListenAndServe() }()
	b.Cleanup(func() {
		_ = p.Close()
		if err := <-errCh; err != nil {
			b.Errorf("ListenAndServe: %v", err)
		}
	})
	var addr string
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if addr = p.Addr(); addr != "" {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if addr == "" {
		b.Fatal("proxy did not start listening")
	}
	proxyURL, err := url.Parse("http://" + addr)
	if err != nil {
		b.Fatal(err)
	}
	through := &http.Client{Transport: &http.Transport{
		Proxy:              http.ProxyURL(proxyURL),
		TLSClientConfig:    &tls.Config{RootCAs: pool},
		DisableCompression: true,
	}}
	direct := &http.Client{Transport: &http.Transport{
		TLSClientConfig:    &tls.Config{InsecureSkipVerify: true},
		DisableCompression: true,
	}}
	sample := func(client *http.Client) time.Duration {
		start := time.Now()
		resp, err := client.Get(upstream.URL)
		if err != nil {
			b.Fatal(err)
		}
		defer resp.Body.Close()
		if _, err := io.Copy(io.Discard, resp.Body); err != nil {
			b.Fatal(err)
		}
		return time.Since(start)
	}
	_ = sample(through)
	_ = sample(direct)
	const n = 20
	proxied := make([]time.Duration, n)
	straight := make([]time.Duration, n)
	for i := 0; i < n; i++ {
		proxied[i] = sample(through)
		straight[i] = sample(direct)
	}
	overhead := medianDuration(proxied) - medianDuration(straight)
	fmt.Printf("warm https median overhead: %s\n", overhead)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = sample(through)
	}
}
```

`medianDuration` uses index `len/2` of the sorted slice. For 20 samples that is index 10. The proxy client keeps its connection. One call of each client is discarded before the 20 samples.

- [ ] **Step 2: Run the benchmark once**

Run: `go test ./internal/proxy/ -run '^$' -bench BenchmarkWarmHTTPS -benchtime=1x -count=1`

Expected: the process exits 0 and prints `warm https median overhead: `. `go test ./internal/proxy/ -count=1` without `-bench` does not run it and still passes.

If the printed overhead is above 10 ms, stop and report the number. Do not change the benchmark into a failing test, and do not drop samples until the number looks better.

- [ ] **Step 3: Commit**

```bash
git add internal/proxy/warm_bench_test.go
git commit -m "$(cat <<'EOF'
test: benchmark warm HTTPS overhead

EOF
)"
```
