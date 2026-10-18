# HTTPProxy Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a single-binary HTTP/HTTPS MITM capture tool (view-only) with Go proxy core, in-memory flow store, WebSocket live updates, and an embedded React UI styled like VS Code/GitHub shell + Chrome DevTools Network workspace.

**Architecture:** One process listens on two ports: `:8080` MITM proxy writes `Flow` events into a ring buffer and broadcasts via a hub; `:8081` serves REST + WebSocket + `go:embed` SPA. Cert manager generates/reuses a local CA and mints per-host leaf certs for CONNECT tunnels. HTTP/1.1 and HTTP/2 are terminated after MITM and re-issued upstream.

**Tech Stack:** Go 1.22+, `crypto/tls`, `net/http`, `golang.org/x/net/http2`, `github.com/google/uuid`, `github.com/gorilla/websocket` (or `coder/websocket`), Vite + React + TypeScript.

**Spec:** `docs/superpowers/specs/2026-10-18-httpproxy-design.md`

## Global Constraints

- View-only v1: no request mutation, no replay, no disk persistence of flows.
- Dual ports: proxy default `:8080`, UI default `:8081`.
- CA files under `~/.httpproxy/` (or `-ca-dir`): `ca.crt`, `ca.key`; first run generates, later reuses.
- Ring buffer default `max-flows=1000`; body cap default `max-body=1048576` bytes per side.
- Protocols: HTTP/1.1 + HTTP/2; no HTTP/3, SOCKS, transparent proxy.
- UI shell: VS Code/GitHub aesthetics; capture workspace: Chrome DevTools Network (table + detail tabs).
- Single executable via `go:embed` of `web` build output.
- Module path: `httpproxy` (local module name in `go.mod`).
- Tests: `go test ./...`; prefer table-driven unit tests + httptest integration for proxy.

---

## File Structure

| Path | Responsibility |
|---|---|
| `go.mod` / `go.sum` | Module and deps |
| `cmd/httpproxy/main.go` | Flags, wire cert/store/proxy/api/ui, run both servers |
| `internal/flow/flow.go` | Shared `Flow`, `Header`, `Summary()` types/JSON tags |
| `internal/store/store.go` | Thread-safe ring buffer |
| `internal/store/store_test.go` | Store unit tests |
| `internal/cert/manager.go` | CA load/create, leaf mint, `GetCertificate` |
| `internal/cert/manager_test.go` | Cert TLS handshake tests |
| `internal/proxy/proxy.go` | Listener, dispatch HTTP vs CONNECT |
| `internal/proxy/http1.go` | Cleartext + MITM HTTP/1.1 forward |
| `internal/proxy/http2.go` | MITM HTTP/2 stream handling |
| `internal/proxy/body.go` | Read body with max truncate |
| `internal/proxy/proxy_test.go` | HTTP/HTTPS/h2 integration tests |
| `internal/api/hub.go` | WS client registry + broadcast |
| `internal/api/server.go` | REST + WS routes |
| `internal/api/server_test.go` | API handler tests |
| `internal/ui/embed.go` | `go:embed dist` + SPA fallback handler |
| `internal/ui/dist/` | Built frontend assets (generated; keep `.gitkeep` until first build) |
| `web/` | Vite React TS app |
| `web/src/App.tsx` | Shell layout |
| `web/src/components/FlowTable.tsx` | Network-style table |
| `web/src/components/FlowDetail.tsx` | Headers/Request/Response tabs |
| `web/src/components/FilterBar.tsx` | Filter inputs |
| `web/src/api.ts` | REST + WS client |
| `web/src/styles.css` | GitHub/VS Code-ish tokens |
| `README.md` | Run, CA install, proxy setup |
| `Makefile` | `make build` = web build + go build |

---

### Task 1: Module scaffold + Flow types

**Files:**
- Create: `go.mod`
- Create: `internal/flow/flow.go`
- Create: `internal/flow/flow_test.go`
- Create: `README.md` (minimal stub; expanded in Task 10)

**Interfaces:**
- Consumes: nothing
- Produces:
  - `type Header struct { Name, Value string }`
  - `type Flow struct { ... }` (all fields from spec §4.1)
  - `func (f Flow) Summary() FlowSummary`
  - `type FlowSummary struct` — same as Flow but without `ReqBody`/`RespBody` byte fields (or with bodies omitted via separate struct)

- [ ] **Step 1: Initialize module**

```bash
cd /home/zhangyw/project/httpproxy
go mod init httpproxy
```

Expected: `go.mod` exists with `module httpproxy` and a Go version ≥ 1.22.

- [ ] **Step 2: Write failing test for Summary omits bodies**

Create `internal/flow/flow_test.go`:

```go
package flow_test

import (
	"testing"
	"httpproxy/internal/flow"
)

func TestSummaryOmitsBodies(t *testing.T) {
	f := flow.Flow{
		ID:      "abc",
		Method:  "GET",
		Host:    "example.com",
		Path:    "/x",
		ReqBody: []byte("secret-req"),
		RespBody: []byte("secret-resp"),
		StatusCode: 200,
	}
	s := f.Summary()
	if s.ID != "abc" || s.Method != "GET" || s.StatusCode != 200 {
		t.Fatalf("unexpected summary meta: %+v", s)
	}
	// Summary type must not expose body fields; if it has them they must be nil/empty
	if len(s.ReqBody) != 0 || len(s.RespBody) != 0 {
		t.Fatalf("summary must not include bodies")
	}
}
```

Note: If `FlowSummary` has no body fields, compile by defining Summary without those fields and delete the body length checks — assert JSON marshal instead:

```go
b, _ := json.Marshal(f.Summary())
if strings.Contains(string(b), "secret-req") || strings.Contains(string(b), "secret-resp") {
	t.Fatal("bodies leaked into summary JSON")
}
```

Prefer the JSON assertion; define `FlowSummary` without body fields.

- [ ] **Step 3: Run test to verify it fails**

```bash
go test ./internal/flow/ -v
```

Expected: FAIL (package or `Summary` undefined).

- [ ] **Step 4: Implement Flow types**

Create `internal/flow/flow.go`:

```go
package flow

import "time"

type Header struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type Flow struct {
	ID           string    `json:"id"`
	Scheme       string    `json:"scheme"`
	Method       string    `json:"method"`
	Host         string    `json:"host"`
	Path         string    `json:"path"`
	HTTPVersion  string    `json:"http_version"`
	StatusCode   int       `json:"status_code"`
	ReqHeaders   []Header  `json:"req_headers"`
	RespHeaders  []Header  `json:"resp_headers"`
	ReqBody      []byte    `json:"req_body,omitempty"`
	RespBody     []byte    `json:"resp_body,omitempty"`
	ReqSize      int64     `json:"req_size"`
	RespSize     int64     `json:"resp_size"`
	ReqTruncated bool      `json:"req_truncated"`
	RespTruncated bool     `json:"resp_truncated"`
	StartedAt    time.Time `json:"started_at"`
	FinishedAt   time.Time `json:"finished_at,omitempty"`
	DurationMs   int64     `json:"duration_ms"`
	Error        string    `json:"error,omitempty"`
	ClientAddr   string    `json:"client_addr"`
}

// FlowSummary is list/WS payload without bodies.
type FlowSummary struct {
	ID            string    `json:"id"`
	Scheme        string    `json:"scheme"`
	Method        string    `json:"method"`
	Host          string    `json:"host"`
	Path          string    `json:"path"`
	HTTPVersion   string    `json:"http_version"`
	StatusCode    int       `json:"status_code"`
	ReqSize       int64     `json:"req_size"`
	RespSize      int64     `json:"resp_size"`
	ReqTruncated  bool      `json:"req_truncated"`
	RespTruncated bool      `json:"resp_truncated"`
	StartedAt     time.Time `json:"started_at"`
	FinishedAt    time.Time `json:"finished_at,omitempty"`
	DurationMs    int64     `json:"duration_ms"`
	Error         string    `json:"error,omitempty"`
	ClientAddr    string    `json:"client_addr"`
}

func (f Flow) Summary() FlowSummary {
	return FlowSummary{
		ID: f.ID, Scheme: f.Scheme, Method: f.Method, Host: f.Host, Path: f.Path,
		HTTPVersion: f.HTTPVersion, StatusCode: f.StatusCode,
		ReqSize: f.ReqSize, RespSize: f.RespSize,
		ReqTruncated: f.ReqTruncated, RespTruncated: f.RespTruncated,
		StartedAt: f.StartedAt, FinishedAt: f.FinishedAt, DurationMs: f.DurationMs,
		Error: f.Error, ClientAddr: f.ClientAddr,
	}
}
```

Adjust the test to use JSON leak assertion if preferred.

- [ ] **Step 5: Run tests**

```bash
go test ./internal/flow/ -v
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add go.mod internal/flow/
git commit -m "feat: add Flow and FlowSummary types"
```

---

### Task 2: In-memory ring store

**Files:**
- Create: `internal/store/store.go`
- Create: `internal/store/store_test.go`

**Interfaces:**
- Consumes: `flow.Flow`, `flow.FlowSummary`
- Produces:
  - `type Filter struct { Host, Path, Status string; Limit int }`
  - `func New(max int) *Store`
  - `func (*Store) Add(f flow.Flow)`
  - `func (*Store) Update(f flow.Flow) bool` — replace by ID; return false if missing
  - `func (*Store) Get(id string) (flow.Flow, bool)`
  - `func (*Store) List(filter Filter) []flow.FlowSummary` — newest first
  - `func (*Store) Clear()`
  - Status filter: empty = all; `"404"` exact; `"4"` means 400–499

- [ ] **Step 1: Write failing tests**

```go
package store_test

import (
	"testing"
	"httpproxy/internal/flow"
	"httpproxy/internal/store"
)

func TestRingEvictsOldest(t *testing.T) {
	s := store.New(2)
	s.Add(flow.Flow{ID: "1", Host: "a"})
	s.Add(flow.Flow{ID: "2", Host: "b"})
	s.Add(flow.Flow{ID: "3", Host: "c"})
	if _, ok := s.Get("1"); ok {
		t.Fatal("id 1 should be evicted")
	}
	list := s.List(store.Filter{})
	if len(list) != 2 || list[0].ID != "3" {
		t.Fatalf("want newest-first [3,2], got %+v", list)
	}
}

func TestFilterHostPathStatus(t *testing.T) {
	s := store.New(10)
	s.Add(flow.Flow{ID: "1", Host: "api.example.com", Path: "/v1/users", StatusCode: 200})
	s.Add(flow.Flow{ID: "2", Host: "cdn.example.com", Path: "/img.png", StatusCode: 404})
	got := s.List(store.Filter{Host: "api", Path: "users", Status: "2"})
	if len(got) != 1 || got[0].ID != "1" {
		t.Fatalf("filter failed: %+v", got)
	}
}

func TestClear(t *testing.T) {
	s := store.New(10)
	s.Add(flow.Flow{ID: "1"})
	s.Clear()
	if len(s.List(store.Filter{})) != 0 {
		t.Fatal("expected empty after clear")
	}
}

func TestUpdateMissingReturnsFalse(t *testing.T) {
	s := store.New(10)
	if s.Update(flow.Flow{ID: "nope"}) {
		t.Fatal("expected false")
	}
}
```

- [ ] **Step 2: Run tests — expect FAIL**

```bash
go test ./internal/store/ -v
```

- [ ] **Step 3: Implement store**

`internal/store/store.go` sketch (implement fully; keep mutex around all ops):

```go
package store

import (
	"strings"
	"sync"
	"httpproxy/internal/flow"
)

type Filter struct {
	Host   string
	Path   string
	Status string
	Limit  int
}

type Store struct {
	mu      sync.RWMutex
	max     int
	order   []string // oldest -> newest IDs
	byID    map[string]flow.Flow
}

func New(max int) *Store {
	if max < 1 {
		max = 1
	}
	return &Store{max: max, byID: make(map[string]flow.Flow)}
}

func (s *Store) Add(f flow.Flow) { /* lock; insert; if len>max delete order[0] */ }
func (s *Store) Update(f flow.Flow) bool { /* lock; replace if exists */ }
func (s *Store) Get(id string) (flow.Flow, bool) { /* RLock */ }
func (s *Store) Clear() { /* reset map and order */ }

func (s *Store) List(filter Filter) []flow.FlowSummary {
	// RLock; iterate order from end to start; apply matchHost/Path/Status; respect Limit
}

func matchStatus(code int, filter string) bool {
	if filter == "" {
		return true
	}
	// if filter is all digits length 3: exact
	// if length 1: code/100 == digit
	// if length 2: code/10 == atoi(filter)
}
```

Implement `matchStatus` exactly as comments; substring match for host/path with `strings.Contains` (case-sensitive is fine for v1).

- [ ] **Step 4: Run tests — expect PASS**

```bash
go test ./internal/store/ -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/store/
git commit -m "feat: add in-memory ring Flow store with filters"
```

---

### Task 3: CA manager

**Files:**
- Create: `internal/cert/manager.go`
- Create: `internal/cert/manager_test.go`

**Interfaces:**
- Consumes: filesystem path for CA dir
- Produces:
  - `func LoadOrCreate(caDir string) (*Manager, error)`
  - `func (*Manager) GetCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error)`
  - `func (*Manager) CACertPEM() []byte`
  - Leaf certs cached by hostname; valid ~24h+; include DNSNames

- [ ] **Step 1: Write failing TLS handshake test**

```go
package cert_test

import (
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"httpproxy/internal/cert"
)

func TestLoadOrCreateAndMITMHandshake(t *testing.T) {
	dir := t.TempDir()
	m, err := cert.LoadOrCreate(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.CACertPEM()) == 0 {
		t.Fatal("empty CA PEM")
	}
	// second load reuses same files
	m2, err := cert.LoadOrCreate(dir)
	if err != nil {
		t.Fatal(err)
	}
	if string(m.CACertPEM()) != string(m2.CACertPEM()) {
		t.Fatal("CA should be stable across loads")
	}

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(m.CACertPEM()) {
		t.Fatal("append CA")
	}
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		GetCertificate: m.GetCertificate,
		NextProtos:     []string{"http/1.1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	}))

	client := &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: pool, ServerName: "example.com"},
	}}
	resp, err := client.Get("https://" + ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	_ = filepath.Join // keep import if using files; remove if unused
	_ = net.Listen
	_ = httptest.NewServer
}
```

Trim unused imports when implementing. Core assertion: client trusting our CA completes TLS to a server using `GetCertificate`.

- [ ] **Step 2: Run — expect FAIL**

```bash
go test ./internal/cert/ -v
```

- [ ] **Step 3: Implement manager**

Requirements in `LoadOrCreate`:
1. `mkdir all` caDir
2. If `ca.crt`+`ca.key` exist, parse PEM; else generate RSA 2048 (or ECDSA P-256) CA with `IsCA=true`, `KeyUsageCertSign`, write files with mode `0600` for key
3. `GetCertificate`: hostname from `hello.ServerName` or fallback `"localhost"`; cache `map[string]*tls.Certificate` with mutex; leaf signed by CA, `DNSNames: []string{host}`

- [ ] **Step 4: Run — expect PASS**

```bash
go test ./internal/cert/ -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/cert/
git commit -m "feat: add local CA manager with dynamic leaf certs"
```

---

### Task 4: Body reader helper + proxy skeleton (cleartext HTTP/1.1)

**Files:**
- Create: `internal/proxy/body.go`
- Create: `internal/proxy/sink.go`
- Create: `internal/proxy/proxy.go`
- Create: `internal/proxy/http1.go`
- Create: `internal/proxy/proxy_test.go`
- Dependencies: `github.com/google/uuid` (`go get`)

**Interfaces:**
- Consumes: `*cert.Manager` (optional until Task 5), `*store.Store` or `FlowSink`
- Produces:
  - `type FlowSink interface { OnCreated(flow.Flow); OnUpdated(flow.Flow) }`
  - `type Proxy struct { ... }`
  - `func New(addr string, sink FlowSink, maxBody int64, ca *cert.Manager) *Proxy`
  - `func (*Proxy) ListenAndServe() error`
  - `func (*Proxy) Close() error`
  - Adapter later: store+hub implement `FlowSink`

`OnCreated`/`OnUpdated` should call `store.Add`/`Update` and broadcast — for Task 4 tests, use a test sink that records flows in a slice.

- [ ] **Step 1: Write failing cleartext proxy test**

```go
func TestProxyCleartextHTTP(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Up", "1")
		w.Write([]byte("hello"))
	}))
	defer upstream.Close()

	sink := &memSink{}
	p := proxy.New("127.0.0.1:0", sink, 1024, nil)
	go p.ListenAndServe()
	defer p.Close()
	addr := waitAddr(p) // helper: expose Addr() on Proxy after listen

	u, _ := url.Parse("http://" + addr)
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(u)}}
	resp, err := client.Get(upstream.URL + "/q")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "hello" {
		t.Fatalf("body %q", body)
	}
	waitFlows(t, sink, 1)
	f := sink.last()
	if f.Method != "GET" || f.StatusCode != 200 || string(f.RespBody) != "hello" {
		t.Fatalf("flow %+v", f)
	}
}
```

Add `Addr() string` on Proxy for tests.

- [ ] **Step 2: Run — expect FAIL**

```bash
go get github.com/google/uuid
go test ./internal/proxy/ -run TestProxyCleartextHTTP -v
```

- [ ] **Step 3: Implement cleartext path**

Behavior:
1. `Listen` TCP; each accepted conn → `bufio.Reader` → `http.ReadRequest`
2. If method `CONNECT` → Task 5 (for now return `501` or skip)
3. Else: parse target from `req.URL` (absolute form) ; create `flow.Flow` with uuid, scheme http, capture headers/body via `readLimited(req.Body, maxBody)`; `sink.OnCreated`; dial upstream with `http.Transport` or manual `net.Dial` + write request; read response; fill flow; `sink.OnUpdated`; write response to client
4. On dial/roundtrip error: set `flow.Error`, still `OnUpdated`, write `502` to client

`readLimited`:

```go
func readLimited(r io.Reader, max int64) (data []byte, size int64, truncated bool, err error) {
	if r == nil {
		return nil, 0, false, nil
	}
	lr := io.LimitReader(r, max+1)
	data, err = io.ReadAll(lr)
	size = int64(len(data))
	if size > max {
		truncated = true
		data = data[:max]
		size = max // also track full size: prefer counting with a custom reader; for v1 set RespSize to len before truncate and document
	}
	// Better: use io.Copy into LimitWriter that counts total — implement a countingLimitedRead that returns (truncatedBytes, totalSize, truncated, err)
	return
}
```

Prefer implementing a small `limitedRead(r, max) (buf []byte, total int64, truncated bool, err error)` that continues discarding after max to consume body (or Close with MaxBytesReader). For proxy correctness, consume full body from client/upstream even when truncating stored copy.

- [ ] **Step 4: Run — expect PASS**

```bash
go test ./internal/proxy/ -run TestProxyCleartextHTTP -v
```

- [ ] **Step 5: Commit**

```bash
git add go.mod go.sum internal/proxy/
git commit -m "feat: add cleartext HTTP/1.1 proxy with flow capture"
```

---

### Task 5: HTTPS MITM (HTTP/1.1 after TLS)

**Files:**
- Modify: `internal/proxy/proxy.go` (CONNECT branch)
- Modify: `internal/proxy/http1.go` (serve TLS conn with http/1.1)
- Modify: `internal/proxy/proxy_test.go`

**Interfaces:**
- Consumes: `*cert.Manager` (required for CONNECT)
- Produces: CONNECT → 200 → `tls.Server` with `GetCertificate` + `NextProtos: h2, http/1.1` → if negotiated `http/1.1`, handle requests in loop like cleartext but scheme=`https`, host from CONNECT/SNI

- [ ] **Step 1: Write failing HTTPS test**

```go
func TestProxyHTTPSMITM(t *testing.T) {
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("secure"))
	}))
	defer upstream.Close()

	caDir := t.TempDir()
	m, err := cert.LoadOrCreate(caDir)
	if err != nil {
		t.Fatal(err)
	}
	sink := &memSink{}
	p := proxy.New("127.0.0.1:0", sink, 1024, m)
	go p.ListenAndServe()
	defer p.Close()

	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(m.CACertPEM())
	proxyURL, _ := url.Parse("http://" + p.Addr())
	client := &http.Client{Transport: &http.Transport{
		Proxy: http.ProxyURL(proxyURL),
		TLSClientConfig: &tls.Config{RootCAs: pool},
	}}
	resp, err := client.Get(upstream.URL + "/s")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if string(b) != "secure" {
		t.Fatalf("got %q", b)
	}
	waitFlows(t, sink, 1)
	f := sink.last()
	if f.Scheme != "https" || f.StatusCode != 200 || string(f.RespBody) != "secure" {
		t.Fatalf("flow %+v body %q", f, f.RespBody)
	}
}
```

Note: `httptest.NewTLSServer` uses its own cert; proxy as TLS client to upstream must skip verify **or** use `upstream.Client()`. Inside proxy upstream transport use `InsecureSkipVerify: true` only for test servers is wrong for production — production must use system roots. Structure:

```go
type Proxy struct {
	UpstreamTLS *tls.Config // nil => system defaults
}
```

In tests, set `p.UpstreamTLS = &tls.Config{InsecureSkipVerify: true}` when hitting httptest TLS, **or** pass custom dial. Document: default `nil` uses system roots; tests inject skip-verify.

- [ ] **Step 2: Run — expect FAIL**

```bash
go test ./internal/proxy/ -run TestProxyHTTPSMITM -v
```

- [ ] **Step 3: Implement CONNECT + MITM HTTP/1.1**

Pseudo-flow:
1. Read CONNECT; reply `HTTP/1.1 200 Connection Established\r\n\r\n`
2. `tls.Server(conn, &tls.Config{GetCertificate: ca.GetCertificate, NextProtos: []string{"h2","http/1.1"}})`
3. Handshake; if ALPN == `h2`, Task 6; else HTTP/1.1 request loop on tls conn
4. For each request: build flow scheme https; round-trip to `https://host/path` with upstream client

- [ ] **Step 4: Run — expect PASS**

```bash
go test ./internal/proxy/ -run TestProxyHTTPSMITM -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/proxy/
git commit -m "feat: add HTTPS MITM for HTTP/1.1 flows"
```

---

### Task 6: HTTP/2 MITM path

**Files:**
- Create: `internal/proxy/http2.go`
- Modify: `internal/proxy/proxy.go` (ALPN branch)
- Modify: `internal/proxy/proxy_test.go`
- Dep: `golang.org/x/net/http2`

**Interfaces:**
- Consumes: TLS server conn with ALPN `h2`
- Produces: each stream → one `Flow`; upstream also HTTP/2 when possible (`http2.Transport` or `tls`+`http2.ClientConn`)

- [ ] **Step 1: Write failing h2 test**

```go
func TestProxyHTTP2(t *testing.T) {
	// Upstream h2 cleartext server using http2.Server + TLS recommended:
	upstream := newH2TLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor != 2 {
			t.Errorf("upstream got proto %s", r.Proto)
		}
		w.Write([]byte("h2ok"))
	}))
	defer upstream.Close()

	m, _ := cert.LoadOrCreate(t.TempDir())
	sink := &memSink{}
	p := proxy.New("127.0.0.1:0", sink, 1024, m)
	p.UpstreamTLS = &tls.Config{InsecureSkipVerify: true, NextProtos: []string{"h2"}}
	go p.ListenAndServe()
	defer p.Close()

	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(m.CACertPEM())
	proxyURL, _ := url.Parse("http://" + p.Addr())
	tr := &http.Transport{
		Proxy:           http.ProxyURL(proxyURL),
		TLSClientConfig: &tls.Config{RootCAs: pool, NextProtos: []string{"h2"}},
		ForceAttemptHTTP2: true,
	}
	http2.ConfigureTransport(tr)
	client := &http.Client{Transport: tr}
	resp, err := client.Get(upstream.URL + "/h2")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if string(b) != "h2ok" {
		t.Fatalf("got %q", b)
	}
	waitFlows(t, sink, 1)
	if sink.last().HTTPVersion != "2" {
		t.Fatalf("want http version 2, got %q", sink.last().HTTPVersion)
	}
}
```

Helper `newH2TLSServer` = TLS listener with `NextProtos: ["h2"]` + `http2.ConfigureServer` + `http.Serve`.

- [ ] **Step 2: Run — expect FAIL**

```bash
go get golang.org/x/net/http2
go test ./internal/proxy/ -run TestProxyHTTP2 -v
```

- [ ] **Step 3: Implement**

On MITM TLS conn with ALPN `h2`:
- `http2.Server{}.ServeConn(tlsConn, &http2.ServeConnOpts{Handler: handler})` where handler captures each request as Flow and forwards via an `http.Client` whose transport is HTTP/2-capable toward real upstream host from `:authority` / Host.

Set `Flow.HTTPVersion = "2"`.

- [ ] **Step 4: Run full proxy suite**

```bash
go test ./internal/proxy/ -v
```

Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add go.mod go.sum internal/proxy/
git commit -m "feat: add HTTP/2 MITM capture path"
```

---

### Task 7: API server (REST + WebSocket hub)

**Files:**
- Create: `internal/api/hub.go`
- Create: `internal/api/server.go`
- Create: `internal/api/server_test.go`
- Create: `internal/api/sink.go` — `StoreSink` adapting store + hub to `proxy.FlowSink`
- Dep: choose `github.com/coder/websocket` or `github.com/gorilla/websocket` (`go get`)

**Interfaces:**
- Consumes: `*store.Store`, `*cert.Manager`
- Produces:
  - `func New(store *store.Store, ca *cert.Manager) *Server`
  - `func (*Server) Handler() http.Handler` — mux for `/api/*` and `/ws/flows`
  - `func (*Server) Sink() proxy.FlowSink`
  - WS event JSON: `{"type":"flow.created"|"flow.updated"|"flows.cleared","flow":Summary?}`

Routes:
- `GET /api/health` → `{"ok":true}`
- `GET /api/flows` → query host, path, status, limit → `[]FlowSummary`
- `GET /api/flows/{id}` → full `Flow` or 404
- `DELETE /api/flows` → clear + broadcast `flows.cleared`
- `GET /api/ca.crt` → `application/x-x509-ca-cert` + PEM bytes
- `GET /ws/flows` → websocket

- [ ] **Step 1: Write API tests**

```go
func TestListGetClearAndCA(t *testing.T) {
	st := store.New(10)
	st.Add(flow.Flow{ID: "1", Host: "h", Path: "/", StatusCode: 200, ReqBody: []byte("x")})
	m, _ := cert.LoadOrCreate(t.TempDir())
	srv := api.New(st, m)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	res, err := http.Get(ts.URL + "/api/flows")
	// decode []; ensure no req_body field or empty
	res, err = http.Get(ts.URL + "/api/flows/1")
	// body contains req_body
	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/flows", nil)
	http.DefaultClient.Do(req)
	res, _ = http.Get(ts.URL + "/api/flows")
	// empty
	res, _ = http.Get(ts.URL + "/api/ca.crt")
	pem, _ := io.ReadAll(res.Body)
	if !bytes.Contains(pem, []byte("BEGIN CERTIFICATE")) {
		t.Fatal("ca pem")
	}
}
```

Add a WS test: connect, call `sink.OnCreated`, read one message with type `flow.created`.

- [ ] **Step 2: Run — expect FAIL**

```bash
go test ./internal/api/ -v
```

- [ ] **Step 3: Implement hub + server + StoreSink**

```go
type StoreSink struct {
	Store *store.Store
	Hub   *Hub
}
func (s *StoreSink) OnCreated(f flow.Flow) {
	s.Store.Add(f)
	s.Hub.Broadcast(Event{Type: "flow.created", Flow: f.Summary()})
}
func (s *StoreSink) OnUpdated(f flow.Flow) {
	s.Store.Update(f)
	s.Hub.Broadcast(Event{Type: "flow.updated", Flow: f.Summary()})
}
```

Use `http.ServeMux` Go 1.22 patterns: `GET /api/flows/{id}`.

- [ ] **Step 4: Run — expect PASS**

```bash
go test ./internal/api/ -v
```

- [ ] **Step 5: Commit**

```bash
git add go.mod go.sum internal/api/
git commit -m "feat: add REST API and WebSocket flow hub"
```

---

### Task 8: UI embed stub + main wiring

**Files:**
- Create: `internal/ui/embed.go`
- Create: `internal/ui/dist/.gitkeep`
- Create: `internal/ui/dist/index.html` (minimal placeholder page until Task 9 replaces via build)
- Create: `cmd/httpproxy/main.go`
- Create: `Makefile`

**Interfaces:**
- Produces: running process with `-proxy`, `-ui`, `-max-flows`, `-max-body`, `-ca-dir`

- [ ] **Step 1: Placeholder embed**

`internal/ui/dist/index.html`:

```html
<!doctype html><title>httpproxy</title><p>UI placeholder — replace with Vite build</p>
```

`internal/ui/embed.go`:

```go
package ui

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed dist/*
var dist embed.FS

func Handler() http.Handler {
	sub, _ := fs.Sub(dist, "dist")
	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" && !strings.Contains(strings.TrimPrefix(r.URL.Path, "/"), ".") {
			r.URL.Path = "/"
		}
		fileServer.ServeHTTP(w, r)
	})
}
```

SPA fallback: if file missing, serve `index.html` (refine in Task 9).

- [ ] **Step 2: Implement main**

```go
package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"httpproxy/internal/api"
	"httpproxy/internal/cert"
	"httpproxy/internal/proxy"
	"httpproxy/internal/store"
	"httpproxy/internal/ui"
)

func main() {
	proxyAddr := flag.String("proxy", ":8080", "proxy listen address")
	uiAddr := flag.String("ui", ":8081", "web UI listen address")
	maxFlows := flag.Int("max-flows", 1000, "max retained flows")
	maxBody := flag.Int64("max-body", 1<<20, "max stored body bytes per side")
	caDir := flag.String("ca-dir", "", "CA directory (default ~/.httpproxy)")
	flag.Parse()

	dir := *caDir
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			log.Fatal(err)
		}
		dir = filepath.Join(home, ".httpproxy")
	}
	m, err := cert.LoadOrCreate(dir)
	if err != nil {
		log.Fatalf("ca: %v", err)
	}
	st := store.New(*maxFlows)
	apiSrv := api.New(st, m)
	p := proxy.New(*proxyAddr, apiSrv.Sink(), *maxBody, m)

	mux := http.NewServeMux()
	mux.Handle("/api/", apiSrv.Handler())
	mux.Handle("/ws/", apiSrv.Handler())
	mux.Handle("/", ui.Handler())

	go func() {
		log.Printf("UI on %s", *uiAddr)
		log.Fatal(http.ListenAndServe(*uiAddr, mux))
	}()
	log.Printf("proxy on %s (CA %s)", *proxyAddr, dir)
	log.Fatal(p.ListenAndServe())
}
```

Fix mux mounting so `api.Handler` owns exact paths (prefer single `apiSrv.Handler()` as root of API and mount UI for non-API). Cleaner: `api` mux includes all API routes; main mounts `mux.Handle("/api/", apiHandler)` and `mux.Handle("/ws/flows", ws)` — adjust `Server.Handler` to return a mux that only registers its routes, then:

```go
root := http.NewServeMux()
root.Handle("/api/", apiSrv.Handler())
root.Handle("/ws/flows", apiSrv.WSHandler())
root.Handle("/", ui.Handler())
```

Expose `WSHandler` or register both inside one `Mount(root)`.

- [ ] **Step 3: Smoke compile**

```bash
go build -o bin/httpproxy ./cmd/httpproxy/
```

Expected: binary created.

- [ ] **Step 4: Manual smoke (optional in agent)**

```bash
./bin/httpproxy -proxy 127.0.0.1:18080 -ui 127.0.0.1:18081 -ca-dir /tmp/httpproxy-ca &
curl -s http://127.0.0.1:18081/api/health
kill %1
```

- [ ] **Step 5: Commit**

```bash
git add cmd/ internal/ui/ Makefile
git commit -m "feat: wire CLI, dual servers, and embedded UI placeholder"
```

---

### Task 9: React frontend (VS Code/GitHub shell + Chrome Network workspace)

**Files:**
- Create: `web/package.json`, `web/vite.config.ts`, `web/tsconfig.json`, `web/index.html`
- Create: `web/src/main.tsx`, `web/src/App.tsx`, `web/src/styles.css`
- Create: `web/src/api.ts`, `web/src/types.ts`
- Create: `web/src/components/TopBar.tsx`, `FilterBar.tsx`, `FlowTable.tsx`, `FlowDetail.tsx`
- Modify: `Makefile` — `build` runs `npm ci && npm run build` then copies to `internal/ui/dist` then `go build`
- Modify: `internal/ui/embed.go` SPA fallback if needed

**UI requirements (from spec):**
- Top bar: title `httpproxy`, proxy address hint, CA download link (`/api/ca.crt`), Clear button
- Filter bar: text (maps to host+path query — send as `host` and/or single `q` split: use separate host/path inputs OR one box writing to both `path` param; implement **host** + **path** + **status** fields)
- Table columns: Name (path), Status, Method, Version, Size, Time — Chrome Network feel (compact rows, selected row highlight)
- Detail: tabs Headers | Request | Response; monospace body; JSON pretty-print try/catch
- Layout default: **top table / bottom detail** split
- Colors: CSS variables — bg `#ffffff` / `#f6f8fa`, border `#d0d7de`, text `#1f2328`, accent `#0969da` (GitHub-like); dense padding like VS Code
- WS: connect `/ws/flows`, on events merge into state; on reconnect `GET /api/flows`
- Dev: `vite.config.ts` proxy `/api` and `/ws` to `http://127.0.0.1:8081`

- [ ] **Step 1: Scaffold Vite app**

```bash
cd /home/zhangyw/project/httpproxy
npm create vite@latest web -- --template react-ts
cd web && npm install
```

- [ ] **Step 2: Implement types + api client**

`web/src/types.ts` — mirror `FlowSummary` / `Flow` JSON fields (snake_case as server).

`web/src/api.ts`:

```ts
export async function listFlows(params: {host?: string; path?: string; status?: string}) {
  const q = new URLSearchParams()
  if (params.host) q.set('host', params.host)
  if (params.path) q.set('path', params.path)
  if (params.status) q.set('status', params.status)
  const r = await fetch(`/api/flows?${q}`)
  return r.json()
}
export async function getFlow(id: string) { /* GET /api/flows/:id */ }
export async function clearFlows() { await fetch('/api/flows', { method: 'DELETE' }) }
export function connectFlowsWS(onEvent: (ev: {type: string; flow?: FlowSummary}) => void): () => void {
  let ws: WebSocket
  let timer: number
  const connect = () => {
    const proto = location.protocol === 'https:' ? 'wss' : 'ws'
    ws = new WebSocket(`${proto}://${location.host}/ws/flows`)
    ws.onmessage = (m) => onEvent(JSON.parse(m.data))
    ws.onclose = () => { timer = window.setTimeout(connect, 1000) }
  }
  connect()
  return () => { clearTimeout(timer); ws.close() }
}
```

- [ ] **Step 3: Implement components + App layout**

Structure in `App.tsx`:
1. state: flows[], selectedId, filters, detail Flow|null
2. useEffect WS + initial list
3. when selectedId changes, fetch detail
4. render TopBar / FilterBar / FlowTable / FlowDetail

- [ ] **Step 4: Build into embed dir**

`web/vite.config.ts`:

```ts
export default defineConfig({
  plugins: [react()],
  base: '/',
  build: { outDir: '../internal/ui/dist', emptyOutDir: true },
  server: {
    proxy: {
      '/api': 'http://127.0.0.1:8081',
      '/ws': { target: 'ws://127.0.0.1:8081', ws: true },
    },
  },
})
```

```bash
cd web && npm run build
go build -o bin/httpproxy ./cmd/httpproxy/
```

Expected: binary serves real UI at `:8081`.

- [ ] **Step 5: Commit**

```bash
git add web/ internal/ui/ Makefile
git commit -m "feat: add React Network-style capture UI embedded in binary"
```

---

### Task 10: README + end-to-end verification

**Files:**
- Modify: `README.md`
- Modify: `Makefile` (document targets)

- [ ] **Step 1: Write README**

Include:
- What it is (view-only MITM)
- Build: `make build` or npm + go build
- Run: `./bin/httpproxy`
- Install CA from UI or `~/.httpproxy/ca.crt`
- Point browser/system proxy to `localhost:8080`
- Open UI `http://localhost:8081`
- Flags table
- Dev mode: run go + `cd web && npm run dev`

- [ ] **Step 2: Run all Go tests**

```bash
go test ./...
```

Expected: PASS.

- [ ] **Step 3: Manual E2E checklist (agent or human)**

1. Start binary  
2. Download CA, trust it  
3. `curl -x http://127.0.0.1:8080 http://example.com` → appears in UI  
4. Browser HTTPS site via proxy → appears with headers/body  
5. Filter by host works; Clear empties table  

- [ ] **Step 4: Commit**

```bash
git add README.md Makefile
git commit -m "docs: add README and build instructions for httpproxy"
```

- [ ] **Step 5: Update spec approval note (optional)**

In design doc §12, change UI revision line to “用户确认 OK”.

---

## Self-Review (plan vs spec)

| Spec requirement | Task |
|---|---|
| View-only MITM HTTP/HTTPS | 4–6 |
| HTTP/2 | 6 |
| Local CA generate/reuse + download | 3, 7 |
| Ring buffer + filter + clear | 2, 7 |
| WebSocket live updates | 7, 9 |
| Dual ports + CLI flags | 8 |
| React embed single binary | 8–9 |
| UI VS Code/GitHub + Chrome Network | 9 |
| Body size limit | 4 (`maxBody`) |
| Tests cert/store/proxy/h2/api | 2–7 |
| README / success criteria | 10 |
| Non-goals excluded | No tasks for replay/SQLite/SOCKS |

**Placeholder scan:** none intentional; implementers must flesh `waitAddr`/`memSink` helpers inside test files as shown by usage.

**Type consistency:** `Flow` / `FlowSummary` in `internal/flow`; store returns summaries; API WS carries `FlowSummary`; proxy emits full `Flow` into sink which stores full and broadcasts summary.

---

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-10-18-httpproxy.md`.

**Two execution options:**

1. **Subagent-Driven (recommended)** — dispatch a fresh subagent per task, review between tasks  
2. **Inline Execution** — execute tasks in this session with executing-plans checkpoints  

Which approach?
