# 代理优先、四面板与 Hosts 改写 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn httpproxy into a proxy-first app with four panels, an optional capture switch that tunnels HTTPS when off, per-profile hosts overrides stored on disk and selected from `config.yaml`, and a home page with live client sources.

**Architecture:** A new `internal/hosts` package parses hosts files and manages `.httpproxy/hosts/*.hosts`. `internal/proxy` consults a compiled table only when dialing upstream, while keeping the original host for SNI, certificate checks, and `Host`. `internal/clients` tracks TCP connections and requests per client IP independently of capture. `internal/api` gains hosts CRUD, activation, overview, and capture-rules endpoints. The React app becomes an activity-bar shell with home, capture, hosts, and settings panes.

**Tech Stack:** Go 1.24.4, `net/http`, `gopkg.in/yaml.v3`, React 19 + TypeScript + Vite, Node built-in test runner, existing CSS tokens.

**Spec:** `docs/superpowers/specs/2026-11-22-proxy-first-design.md`

## Global Constraints

- Go 1.24.4; no new Go or npm dependencies.
- New installs write `capture: false`; existing `config.yaml` keeps its value. `Defaults()` becomes `capture: false`.
- `hosts_profile` defaults to `""` (disabled) and is written by `MarshalYAML`.
- Capture rules remain `capture_empty: all|none`, `capture_https: decrypt|tunnel`, `capture_hosts: []`.
- Hosts matching is exact domain → IP only: no wildcards, no ports, no regex. IPv4 and IPv6 are supported.
- Duplicate domain in one hosts file is not an error: first occurrence wins, later ones are ignored with a warning.
- Files: `<workdir>/.httpproxy/hosts/<name>.hosts`, UTF-8, max 1 MiB, max 1000 mappings; profile names 1–64 chars.
- Hosts override changes only the dial address. TLS `ServerName`, certificate verification, and `Host` stay the original domain.
- The proxy's own listen address must be refused after mapping too.
- Client stats are per-IP, in memory, 2-second polling, independent of `capture`; restart clears them.
- All new HTTP error text is Chinese; startup/config parse errors stay English. Keep the literal `下载 CA` in the frontend (embedded UI test checks it).
- Reuse existing CSS variables and components; no new colors. Existing `.topbar` is removed.
- API paths and JSON field names are exactly those in the spec and this plan.

## Review Focus

1. **Duplicate and externally edited hosts files** — first entry wins, warnings are non-blocking, and editing the active file outside the UI reloads on the next hosts/overview request without breaking the running proxy. Tests: Task 1 `TestParseFirstDuplicateWins`, Task 8 `TestHostsRefreshOnList`.
2. **Mapped IP with HTTPS** — SNI and certificate verification still use the original domain for MITM, and the tunnel path carries untouched TLS. Tests: Task 5 `TestHostsOverrideHTTPSKeepsSNI`, `TestHostsOverrideTunnel`.
3. **Toggling capture on an already-open HTTP/2 or keep-alive MITM connection** — pause stops new flows immediately; resume records again; the connection keeps forwarding. Test: Task 4 `TestPauseCaptureStopsRecordingOnOpenMITMConnection`.
4. **Loopback and IPv6 clients** — default `127.0.0.1`/`::1` clients must aggregate, count active connections, and not double-count requests; abrupt closes must not leak counts. Tests: Task 6 `TestTrackerCounts`, `TestProxyRecordsClients`.
5. **Self-proxy after mapping, and port/IPv6 formatting** — mapping a name to the proxy's own listener returns 502, and IPv6 targets use `net.JoinHostPort`. Tests: Task 5 `TestHostsSelfProxyRejected`, Task 1 `TestParseMapsAndLooksUp`.

---

### Task 1: Hosts parser and lookup table

**Files:**
- Create: `internal/hosts/hosts.go`
- Test: `internal/hosts/hosts_test.go`

**Interfaces:**
- Consumes: nothing
- Produces:
  ```go
  package hosts

  type Warning struct {
      Domain      string `json:"domain"`
      IgnoredLine int    `json:"ignored_line"`
      FirstLine   int    `json:"first_line"`
  }

  type ParseError struct {
      Line int
      Msg  string
  }
  func (e *ParseError) Error() string

  type Table struct { /* map[string]net.IP */ }

  const (
      MaxFileBytes = 1 << 20
      MaxMappings  = 1000
      MaxHostLen   = 253
  )

  func Parse(data []byte) (Table, []Warning, error)
  func (t Table) Lookup(host string) (net.IP, bool)
  func (t Table) Len() int
  ```

- [ ] **Step 1: Write failing parser tests**

Create `internal/hosts/hosts_test.go` with these tests:

```go
func TestParseMapsAndLooksUp(t *testing.T) {
    data := []byte(`
# local
10.0.0.5   api.example.com
10.0.0.6   cdn.example.com static.example.com  # trailing comment
::1        v6.example.com

127.0.0.1  Example.COM.
`)
    table, warnings, err := Parse(data)
    if err != nil { t.Fatal(err) }
    if len(warnings) != 0 { t.Fatalf("warnings: %+v", warnings) }
    want := map[string]string{
        "api.example.com": "10.0.0.5",
        "cdn.example.com": "10.0.0.6",
        "static.example.com": "10.0.0.6",
        "v6.example.com": "::1",
        "example.com": "127.0.0.1",
    }
    if table.Len() != len(want) { t.Fatalf("Len()=%d", table.Len()) }
    for host, ip := range want {
        got, ok := table.Lookup(host)
        if !ok || got.String() != ip { t.Fatalf("%s => %v %v", host, got, ok) }
    }
    if _, ok := table.Lookup("missing.example.com"); ok { t.Fatal("unexpected hit") }
}

func TestParseFirstDuplicateWins(t *testing.T) {
    table, warnings, err := Parse([]byte("10.0.0.5 api.example.com\n10.0.0.6 api.example.com\n"))
    if err != nil { t.Fatal(err) }
    if got, _ := table.Lookup("api.example.com"); got.String() != "10.0.0.5" {
        t.Fatalf("got %s", got)
    }
    if table.Len() != 1 { t.Fatalf("Len()=%d", table.Len()) }
    if len(warnings) != 1 || warnings[0] != (Warning{Domain: "api.example.com", IgnoredLine: 2, FirstLine: 1}) {
        t.Fatalf("warnings: %+v", warnings)
    }
}

func TestParseRejectsInvalid(t *testing.T) {
    cases := []struct{ name, data string; line int; msg string }{
        {"missing ip", "api.example.com\n", 1, "missing field"},
        {"bad ip", "999.1.1.1 api.example.com\n", 1, "invalid IP"},
        {"bad name", "10.0.0.5 bad_host.example.com\n", 1, "invalid hostname"},
        {"wildcard", "10.0.0.5 *.example.com\n", 1, "invalid hostname"},
        {"port", "10.0.0.5 api.example.com:8443\n", 1, "invalid hostname"},
        {"ip name", "10.0.0.5 10.0.0.6\n", 1, "invalid hostname"},
        {"bad line 3", "\n\n10.0.0.5\n", 3, "missing field"},
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            _, _, err := Parse([]byte(tc.data))
            var pe *ParseError
            if !errors.As(err, &pe) { t.Fatalf("err %v", err) }
            if pe.Line != tc.line || pe.Msg != tc.msg { t.Fatalf("%+v", pe) }
        })
    }
}

func TestParseLimitsAndCRLF(t *testing.T) {
    big := bytes.Repeat([]byte("a"), MaxFileBytes+1)
    if _, _, err := Parse(big); err == nil { t.Fatal("size limit not enforced") }

    var b bytes.Buffer
    for i := 0; i < MaxMappings+1; i++ {
        fmt.Fprintf(&b, "10.0.0.1 h%d.example.com\r\n", i)
    }
    _, _, err := Parse(b.Bytes())
    var pe *ParseError
    if !errors.As(err, &pe) || pe.Msg != "too many mappings" { t.Fatalf("err %v", err) }

    table, _, err := Parse([]byte("10.0.0.1 api.example.com\r\n"))
    if err != nil { t.Fatal(err) }
    if _, ok := table.Lookup("api.example.com"); !ok { t.Fatal("CRLF line not parsed") }
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/hosts -run 'TestParse|TestLookup' -v`  
Expected: FAIL because package/file does not exist.

- [ ] **Step 3: Implement `internal/hosts/hosts.go`**

Rules:
- `Parse` rejects `len(data) > MaxFileBytes` with `ParseError{Line: 0, Msg: "file exceeds 1 MiB"}`.
- Split on `\n`; trim a trailing `\r`; strip from the first `#`; skip blank lines.
- `strings.Fields`; fewer than 2 fields → `ParseError{line, "missing field"}`.
- Parse `fields[0]` with `net.ParseIP`; invalid → `"invalid IP"`.
- For each name in `fields[1:]`: lower-case and trim one trailing dot; reject if empty, > `MaxHostLen`, contains `*`, `:`, `/`, or parses as an IP, or fails the DNS label rules (implement the same label checks locally in `internal/hosts`; do not import `internal/capture`); invalid → `"invalid hostname"`.
- Count mappings; more than `MaxMappings` → `ParseError{line, "too many mappings"}`.
- First mapping for a domain wins; later ones append `Warning{Domain, line, firstLine}` and are skipped.
- `Lookup` normalizes the same way; zero `Table` returns `false`.
- `Len` returns unique domains.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/hosts -v`  
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/hosts
git commit -m "feat: parse hosts files into an exact lookup table"
```

---

### Task 2: Hosts profile files

**Files:**
- Create: `internal/hosts/profiles.go`
- Test: `internal/hosts/profiles_test.go`

**Interfaces:**
- Consumes: Task 1 `hosts.ValidName` rules and constants.
- Produces:
  ```go
  var (
      ErrInvalidName = errors.New("invalid profile name")
      ErrExists      = errors.New("profile already exists")
      ErrNotFound    = errors.New("profile not found")
  )

  type Profiles struct{ /* dir string */ }

  func OpenProfiles(root string) (*Profiles, error) // root is <workdir>/.httpproxy
  func ValidName(name string) bool
  func (p *Profiles) Dir() string
  func (p *Profiles) List() ([]string, error)
  func (p *Profiles) Read(name string) ([]byte, error)
  func (p *Profiles) Write(name string, data []byte) error
  func (p *Profiles) Create(name string) error
  func (p *Profiles) Rename(oldName, newName string) error
  func (p *Profiles) Delete(name string) error
  ```

- [ ] **Step 1: Write failing profile-store tests**

Create `internal/hosts/profiles_test.go` covering:

```go
func TestOpenProfilesCreatesHostsDir(t *testing.T) {
    root := t.TempDir()
    p, err := OpenProfiles(root)
    if err != nil { t.Fatal(err) }
    info, err := os.Stat(filepath.Join(root, "hosts"))
    if err != nil || !info.IsDir() { t.Fatalf("dir: %v", err) }
    if info.Mode().Perm() != 0o700 { t.Fatalf("mode %o", info.Mode().Perm()) }
}

func TestProfilesCRUD(t *testing.T) {
    p, _ := OpenProfiles(t.TempDir())
    if err := p.Create("测试 环境"); err != nil { t.Fatal(err) }
    if err := p.Write("测试 环境", []byte("10.0.0.5 api.example.com\n")); err != nil { t.Fatal(err) }
    data, err := p.Read("测试 环境")
    if err != nil || string(data) != "10.0.0.5 api.example.com\n" { t.Fatalf("%q %v", data, err) }
    if err := p.Rename("测试 环境", "预发"); err != nil { t.Fatal(err) }
    if _, err := p.Read("测试 环境"); !errors.Is(err, ErrNotFound) { t.Fatalf("old: %v", err) }
    if err := p.Delete("预发"); err != nil { t.Fatal(err) }
    if err := p.Delete("预发"); !errors.Is(err, ErrNotFound) { t.Fatalf("delete: %v", err) }
}

func TestProfilesListSortsAndIgnores(t *testing.T) {
    p, _ := OpenProfiles(t.TempDir())
    for _, name := range []string{"b", "A", "c"} {
        if err := p.Create(name); err != nil { t.Fatal(err) }
    }
    os.WriteFile(filepath.Join(p.Dir(), "notes.txt"), []byte("x"), 0o644)
    os.Mkdir(filepath.Join(p.Dir(), "dir.hosts"), 0o755)
    os.WriteFile(filepath.Join(p.Dir(), ".hidden.hosts"), []byte("x"), 0o644)
    got, err := p.List()
    if err != nil { t.Fatal(err) }
    if !reflect.DeepEqual(got, []string{"A", "b", "c"}) { t.Fatalf("%v", got) }
}

func TestProfilesRejectsNamesAndDuplicates(t *testing.T) {
    p, _ := OpenProfiles(t.TempDir())
    for _, name := range []string{"", ".hidden", "a/b", "..", strings.Repeat("x", 65)} {
        if err := p.Create(name); !errors.Is(err, ErrInvalidName) { t.Fatalf("%q => %v", name, err) }
    }
    if err := p.Create("Test"); err != nil { t.Fatal(err) }
    if err := p.Create("test"); !errors.Is(err, ErrExists) { t.Fatalf("case duplicate: %v", err) }
    if err := p.Rename("Test", "TEST"); !errors.Is(err, ErrExists) { t.Fatalf("rename duplicate: %v", err) }
}
```

Also assert `List` skips invalid `.hosts` names and files not ending in `.hosts`; `Write` is atomic (write over existing leaves either old or new bytes, not partial; test the final bytes and mode `0644`).

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/hosts -run 'TestOpenProfiles|TestProfiles' -v`  
Expected: FAIL because `profiles.go` does not exist.

- [ ] **Step 3: Implement `internal/hosts/profiles.go`**

- `ValidName`: trim; 1–64 runes; not `.`/`..`; first rune not `.`; each rune must be `unicode.IsLetter`, `unicode.IsDigit`, space, `-`, `_`, or `.`; reject `/`, `\`, control characters.
- `OpenProfiles`: `os.MkdirAll(filepath.Join(root, "hosts"), 0o700)`.
- `List`: `os.ReadDir`; keep regular files ending in `.hosts`, not starting `.`, whose base name passes `ValidName`; sort with `strings.ToLower` then raw name as tiebreak.
- `Read`/`Write`/`Delete`: validate name; map `os.IsNotExist` to `ErrNotFound`; `Write` uses `os.CreateTemp` in the hosts dir, `Chmod(0644)`, write, close, `os.Rename`.
- `Create`: `os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)`; map `os.IsExist` to `ErrExists`.
- `Rename`: validate both; reject if either exists case-insensitively (use `List`); `os.Rename`; map not-exist to `ErrNotFound`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/hosts -v`  
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/hosts
git commit -m "feat: manage hosts profiles on disk"
```

---

### Task 3: Config field, default capture off, session setter

**Files:**
- Modify: `internal/config/value.go`
- Modify: `internal/config/session.go`
- Modify: `internal/config/value_test.go`
- Modify: `internal/config/session_test.go`
- Modify: `internal/api/config_test.go` (add `hosts_profile` to every `/api/config` body)

**Interfaces:**
- Consumes: nothing.
- Produces:
  ```go
  // config.Values
  HostsProfile string `json:"hosts_profile" yaml:"hosts_profile"`

  var ErrHostsProfile = errors.New("hosts_profile must be a string")

  func (s *Session) SetHostsProfile(name string, apply func(Applied)) (Applied, error)
  ```

- [ ] **Step 1: Write failing config tests**

Add to `internal/config/value_test.go`:

```go
func TestDefaultsCaptureOffAndHostsProfileEmpty(t *testing.T) {
    got := Defaults()
    if got.Capture { t.Fatal("capture should default to false") }
    if got.HostsProfile != "" { t.Fatalf("hosts_profile = %q", got.HostsProfile) }
    text := string(MarshalYAML(got))
    if !strings.Contains(text, "hosts_profile: \"\"\n") { t.Fatalf("%s", text) }
}

func TestParseYAMLDefaultsHostsProfile(t *testing.T) {
    raw := []byte("proxy: \"127.0.0.1:8080\"\nui: \"127.0.0.1:8081\"\nmax_flows: 1\nmax_body: 1\ntheme: dark\n")
    got, err := ParseYAML(raw)
    if err != nil { t.Fatal(err) }
    if got.HostsProfile != "" { t.Fatalf("%q", got.HostsProfile) }
    if got.Capture { t.Fatal("capture default") }
}

func TestParseJSONRequiresAndReadsHostsProfile(t *testing.T) {
    base := `{"proxy":"127.0.0.1:8080","ui":"127.0.0.1:8081","max_flows":1,"max_body":1,"theme":"dark","capture":false,"capture_empty":"all","capture_https":"decrypt","capture_hosts":[],"hosts_profile":"测试"}`
    got, err := ParseJSON([]byte(base))
    if err != nil || got.HostsProfile != "测试" { t.Fatalf("%+v %v", got, err) }
    missing := strings.Replace(base, `"hosts_profile":"测试"`, `"hosts_profile":null`, 1)
    if _, err := ParseJSON([]byte(missing)); !errors.Is(err, ErrMissingField) { t.Fatalf("%v", err) }
}
```

Update `TestParseYAMLDefaultsCaptureFields` to assert `!got.Capture`. Add `hosts_profile` to every `TestParseJSON` valid body, to `TestDefaultsAndRoundTrip`'s ordered key list, and to every JSON body in `internal/api/config_test.go`; otherwise the required-field check fails.

Add to `internal/config/session_test.go`:

```go
func TestSetHostsProfile(t *testing.T) {
    d, file, err := Open(t.TempDir())
    if err != nil { t.Fatal(err) }
    s := NewSession(d, file, Overrides{}, file.Proxy, file.UI, file.MaxFlows, file.MaxBody)
    next := file
    next.HostsProfile = "测试环境"
    applied, err := s.SetHostsProfile("测试环境", nil)
    if err != nil { t.Fatal(err) }
    if applied.File.HostsProfile != "测试环境" || applied.Runtime.HostsProfile != "测试环境" {
        t.Fatalf("%+v", applied)
    }
    data, err := os.ReadFile(filepath.Join(d.Path(), "config.yaml"))
    if err != nil { t.Fatal(err) }
    fromDisk, err := ParseYAML(data)
    if err != nil || fromDisk.HostsProfile != "测试环境" { t.Fatalf("%+v %v", fromDisk, err) }
    if applied.File.Proxy != file.Proxy || applied.File.MaxFlows != file.MaxFlows {
        t.Fatalf("other fields changed: %+v", applied.File)
    }
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/config -run 'HostsProfile|CaptureOff' -v`  
Expected: FAIL because `HostsProfile` and `SetHostsProfile` do not exist.

- [ ] **Step 3: Implement**

- `Defaults`: `Capture: false`, add `HostsProfile: ""`.
- `knownFields`: add `hosts_profile`.
- `ParseYAML`: default `""`; if the key exists and is not null, `yamlString` else `ErrHostsProfile`.
- `ParseJSON`: if present and not null, `jsonString` else `ErrHostsProfile`; add `hosts_profile` to the required-key loop.
- `MarshalYAML`: append `hosts_profile: "<quoted>"`.
- `Session.SetHostsProfile`: lock; `next := s.file`; set field; `saveLocked(next, apply)`.
- `saveLocked`: `s.runtime.HostsProfile = next.HostsProfile`.
- Keep `CaptureHosts` clone logic.

- [ ] **Step 4: Run the full Go suite**

Run: `go test ./...`  
Expected: PASS. If an old test assumes `capture: true`, update the expectation to `false`; do not relax the parser.

- [ ] **Step 5: Commit**

```bash
git add internal/config internal/api/config_test.go
git commit -m "feat: persist hosts profile and default capture off"
```

---

### Task 4: Capture off tunnels HTTPS; pause takes effect per request

**Files:**
- Modify: `internal/capture/policy.go`
- Modify: `internal/capture/policy_test.go`
- Modify: `internal/proxy/http1.go`
- Modify: `internal/proxy/http2.go`
- Test: `internal/proxy/protocol_test.go`

**Interfaces:**
- Consumes: `capture.Policy.Decide(https bool, authority string) Action`.
- Produces:
  - `Decide` returns `Tunnel` for HTTPS when `capture == false`, regardless of `capture_https`.
  - `handleHTTP2(conn *tls.Conn, connectAuthority, upstreamSNI string)` (drop the `record` parameter).
  - Per-request/per-stream record decision inside `handleConnect`'s HTTP/1.1 loop and `handleHTTP2Request`.

- [ ] **Step 1: Write failing policy tests**

Change `TestDecideEmptyListAndSwitch` expectations:

```go
off, _ := Compile(false, "all", "decrypt", []string{"example.com"})
if off.Decide(true, "example.com:443") != Tunnel { t.Fatal("capture off must tunnel HTTPS") }
if off.Decide(false, "example.com") != DecryptOnly { t.Fatal("plain HTTP still forwards without recording") }

offTunnel, _ := Compile(false, "all", "tunnel", nil)
if offTunnel.Decide(true, "example.com:443") != Tunnel { t.Fatal("capture off ignores capture_https") }
```

- [ ] **Step 2: Add the pause-mid-connection proxy test**

In `internal/proxy/protocol_test.go` add:

```go
func TestPauseCaptureStopsRecordingOnOpenMITMConnection(t *testing.T) {
    upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        _, _ = io.WriteString(w, "ok")
    }))
    defer upstream.Close()

    manager, pool := newCA(t)
    sink := &memSink{}
    p := startProxy(t, sink, 1024, manager)
    target := upstream.Listener.Addr().String()
    host, _, _ := net.SplitHostPort(target)
    tlsConn, err := dialMITM(t, p.Addr(), target, &tls.Config{RootCAs: pool, ServerName: host, NextProtos: []string{"http/1.1"}})
    if err != nil { t.Fatal(err) }

    on, _ := capture.Compile(true, "all", "decrypt", nil)
    off, _ := capture.Compile(false, "all", "decrypt", nil)
    p.SetPolicy(on)

    reader := bufio.NewReader(tlsConn)
    send := func() {
        t.Helper()
        if _, err := io.WriteString(tlsConn, "GET / HTTP/1.1\r\nHost: "+target+"\r\n\r\n"); err != nil { t.Fatal(err) }
        resp, err := http.ReadResponse(reader, &http.Request{Method: http.MethodGet})
        if err != nil { t.Fatal(err) }
        _, _ = io.Copy(io.Discard, resp.Body)
        _ = resp.Body.Close()
    }

    send()
    waitFlowCount(t, sink, 1)

    p.SetPolicy(off)
    send()
    quietUntil := time.Now().Add(200 * time.Millisecond)
    for time.Now().Before(quietUntil) {
        if _, updated := sink.counts(); updated != 1 { t.Fatalf("pause recorded a flow: %d", updated) }
        time.Sleep(5 * time.Millisecond)
    }

    p.SetPolicy(on)
    send()
    waitFlowCount(t, sink, 2)
}
```

Note: the test must use the same `bufio.Reader` across requests; the snippet above does.

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/capture ./internal/proxy -run 'TestDecideEmptyListAndSwitch|TestPauseCapture' -v`  
Expected: FAIL on the new expectations.

- [ ] **Step 4: Implement**

- `Policy.Decide`:
  ```go
  if !p.capture {
      if https { return Tunnel }
      return DecryptOnly
  }
  if p.records(https, authority) { return Record }
  if https && p.tunnel { return Tunnel }
  return DecryptOnly
  ```
- `handleConnect`: keep the initial `action` for the CONNECT decision. In the HTTP/1.1 loop over the TLS connection, compute `record := p.currentPolicy().Decide(true, connectAuthority) == capture.Record` for each request.
- `handleHTTP2`: remove the `record` parameter; `handleHTTP2Request` computes `record := p.currentPolicy().Decide(true, connectAuthority) == capture.Record` per stream.
- `handleHTTP1` already computes `record` per plain request; unchanged.
- Existing `TestDecryptOnly...` tests may need their policy setup reviewed; run the full proxy suite and fix only expectations that encoded the old capture-off semantics.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/capture ./internal/proxy -v`  
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/capture internal/proxy
git commit -m "feat: tunnel HTTPS when capture is off and pause per request"
```

---

### Task 5: Hosts-aware upstream dialing

**Files:**
- Modify: `internal/flow/flow.go`
- Modify: `internal/proxy/proxy.go`
- Modify: `internal/proxy/http1.go`
- Modify: `internal/proxy/http2.go`
- Test: `internal/proxy/hosts_test.go`

**Interfaces:**
- Consumes: `hosts.Table`, `hosts.Lookup`, `hosts.Parse`.
- Produces:
  ```go
  // flow.Flow
  UpstreamAddr string `json:"upstream_addr,omitempty"`
  HostsProfile string `json:"hosts_profile,omitempty"`

  // proxy.Proxy
  func (p *Proxy) SetHosts(name string, table hosts.Table)
  func (p *Proxy) hostsForURL(u *url.URL) (addr, profile string, ok bool)
  func (p *Proxy) dialContext(ctx context.Context, network, addr string) (net.Conn, error)
  ```

- [ ] **Step 1: Write failing hosts-routing tests**

Create `internal/proxy/hosts_test.go`:

```go
func TestHostsOverridePlainHTTP(t *testing.T) {
    var gotHost string
    upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        gotHost = r.Host
        _, _ = io.WriteString(w, "mapped")
    }))
    defer upstream.Close()
    _, port, _ := net.SplitHostPort(upstream.Listener.Addr().String())

    table, _, err := hosts.Parse([]byte("127.0.0.1 api.test\n"))
    if err != nil { t.Fatal(err) }

    sink := &memSink{}
    p := startProxy(t, sink, 1024, nil)
    p.SetHosts("测试", table)

    resp, err := proxyClient(t, p.Addr()).Get("http://api.test:" + port + "/x")
    if err != nil { t.Fatal(err) }
    body, _ := io.ReadAll(resp.Body)
    _ = resp.Body.Close()
    if resp.StatusCode != http.StatusOK || string(body) != "mapped" { t.Fatalf("%d %q", resp.StatusCode, body) }
    if gotHost != "api.test:"+port { t.Fatalf("Host = %q", gotHost) }

    f := waitUpdatedFlow(t, sink)
    if f.UpstreamAddr != "127.0.0.1:"+port || f.HostsProfile != "测试" {
        t.Fatalf("upstream=%q profile=%q", f.UpstreamAddr, f.HostsProfile)
    }
}
```

Additional tests:

- `TestHostsOverrideHTTPSKeepsSNI`: TLS upstream handler records `r.TLS.ServerName`; map `api.test` to `127.0.0.1`; `dialMITM` to `api.test:<port>` with client `ServerName: api.test` and proxy CA pool; assert response, `sni == "api.test"`, flow fields.
- `TestHostsOverrideTunnel`: policy `Compile(true,"all","tunnel",nil)`; map `api.test`; raw CONNECT + client TLS handshake to `api.test` with `InsecureSkipVerify: true` (or the upstream's test roots); assert body and `sink.counts()` remain `(0,0)`.
- `TestHostsSelfProxyRejected`: start proxy; map `loop.test` to the proxy's `127.0.0.1:<port>`; plain request through proxy to `http://loop.test:<port>/`; expect 502 (a failed Flow is acceptable because recording is on).
- `TestSetHostsClosesIdleConnections`: upstream A returns "A", upstream B returns "B"; map `api.test` to A; make one request with a keep-alive proxy client; `SetHosts` to B; make the same request; assert "B". Use `http.Client{Transport:&http.Transport{Proxy:http.ProxyURL(proxyURL)}}` (keep-alives enabled).

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/proxy -run 'TestHosts|TestSetHosts' -v`  
Expected: FAIL because `SetHosts`/flow fields do not exist.

- [ ] **Step 3: Implement**

- Add `hostsName string` and `hostsTable hosts.Table` to `Proxy`, guarded by `p.mu`.
- `SetHosts`: set fields under lock; copy the current `transport` and `sniTransports`; unlock; call `CloseIdleConnections()` on each non-nil transport.
- `hostsForURL`: if `u.Hostname()` is an IP literal, not found; determine port (explicit, else 80/443 by scheme); look up; return `net.JoinHostPort(ip.String(), port)`, profile name.
- `dialContext`: split host/port; if `hostsForURL`-equivalent lookup hits, target the mapped `IP:port`; check `isSelfAddr(target)` and return `errSelfProxy`; otherwise dial the original `addr`. Use `net.Dialer{Timeout: 30s, KeepAlive: 30s}`.
- `buildTransport`: `DialContext: p.dialContext`.
- `sniTransport`: replace the inline dialer with `p.dialContext(ctx, network, dialHost)` before `tls.Client`; keep the existing SNI/handshake logic.
- `spliceTunnel`: use `p.dialContext(context.Background(), "tcp", authority)`; if `errors.Is(err, errSelfProxy)` write the existing self-proxy 502 text, else `Bad Gateway`.
- Refactor `isLoopbackTarget` to share `isSelfAddr(hostport string) bool` with the dial path.
- In `handleRequest` and `handleHTTP2Request`, after the URL is set and only when `record`, call `hostsForURL` and fill `f.UpstreamAddr`/`f.HostsProfile`.
- Do not change `req.URL.Host`, the `Host` header, TLS `ServerName`, or certificate verification.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/proxy -run 'TestHosts|TestSetHosts' -v`  
Expected: PASS.

- [ ] **Step 5: Run the full proxy suite**

Run: `go test ./internal/proxy -v`  
Expected: PASS; existing tests still use the empty table.

- [ ] **Step 6: Commit**

```bash
git add internal/flow internal/proxy
git commit -m "feat: route upstream dials through hosts profiles"
```

---

### Task 6: Client tracker and proxy wiring

**Files:**
- Create: `internal/clients/tracker.go`
- Test: `internal/clients/tracker_test.go`
- Modify: `internal/proxy/proxy.go`
- Modify: `internal/proxy/http1.go`
- Modify: `internal/proxy/http2.go`
- Test: `internal/proxy/clients_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  ```go
  package clients

  type Client struct {
      IP          string    `json:"ip"`
      ActiveConns int       `json:"active_conns"`
      Requests    int       `json:"requests"`
      Tunnels     int       `json:"tunnels"`
      LastSeen    time.Time `json:"last_seen"`
      Hosts       []string  `json:"hosts"`
  }

  func New() *Tracker
  func NewWithClock(now func() time.Time) *Tracker
  func (t *Tracker) AddConn(ip string)
  func (t *Tracker) DropConn(ip string)
  func (t *Tracker) Request(ip, host string)
  func (t *Tracker) Tunnel(ip, host string)
  func (t *Tracker) Snapshot() []Client

  // proxy.Proxy
  func (p *Proxy) SetClientTracker(t *clients.Tracker)
  ```

- [ ] **Step 1: Write failing tracker tests**

```go
func TestTrackerCounts(t *testing.T) {
    now := time.Unix(1000, 0)
    tr := NewWithClock(func() time.Time { return now })
    tr.AddConn("127.0.0.1")
    now = now.Add(time.Second)
    tr.AddConn("::1")
    now = now.Add(time.Second)
    tr.Request("127.0.0.1", "api.example.com")
    now = now.Add(time.Second)
    tr.Tunnel("::1", "v6.example.com")
    tr.DropConn("127.0.0.1")
    got := tr.Snapshot()
    if len(got) != 2 { t.Fatalf("%+v", got) }
    if got[0].IP != "::1" || got[0].Tunnels != 1 || got[0].ActiveConns != 1 { t.Fatalf("%+v", got[0]) }
    if got[1].IP != "127.0.0.1" || got[1].Requests != 1 || got[1].ActiveConns != 0 { t.Fatalf("%+v", got[1]) }
}

func TestTrackerHostsAndPrune(t *testing.T) {
    now := time.Unix(1000, 0)
    tr := NewWithClock(func() time.Time { return now })
    for i := 0; i < 10; i++ { tr.Request("1.2.3.4", fmt.Sprintf("h%d.example.com", i)) }
    got := tr.Snapshot()[0]
    if len(got.Hosts) != 8 || got.Hosts[0] != "h9.example.com" { t.Fatalf("%+v", got.Hosts) }
    now = now.Add(4 * time.Minute)
    tr.Request("5.6.7.8", "fresh.example.com")
    now = now.Add(2 * time.Minute)
    got = tr.Snapshot()
    if len(got) != 1 || got[0].IP != "5.6.7.8" { t.Fatalf("%+v", got) }
}
```

Also assert the 200-client cap drops the oldest.

- [ ] **Step 2: Write the failing proxy integration test**

In `internal/proxy/clients_test.go`, start a proxy with a tracker, make a plain request, and assert `tracker.Snapshot()` has `127.0.0.1` with `Requests >= 1` and the host in `Hosts`. Add a tunnel-mode CONNECT test asserting `Tunnels == 1` and the domain in `Hosts`.

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/clients ./internal/proxy -run 'TestTracker|TestProxyRecordsClients' -v`  
Expected: FAIL because the package and wiring do not exist.

- [ ] **Step 4: Implement**

- `Tracker` with `mu`, `byIP`, `now`. `AddConn`/`DropConn` adjust `ActiveConns`; `Request`/`Tunnel` increment and call `addHost` (dedupe, newest first, max 8).
- `Snapshot`: lock; prune entries with `ActiveConns == 0 && now.Sub(LastSeen) > 5*time.Minute`; sort by `LastSeen` desc with IP ascending as the tiebreak; cap 200; return deep copies (copy `Hosts`).
- Proxy: add `tracker *clients.Tracker` and `SetClientTracker`.
- On accept, compute `ip` via `net.SplitHostPort(conn.RemoteAddr().String())`; `AddConn` and defer `DropConn`.
- `handleRequest`: `p.clientRequest(conn, host)` counts one request; `handleHTTP2Request`: count one request with `req.RemoteAddr`.
- `handleConnect`: count one tunnel with the connect host (strip port).
- Use `hostWithoutPort` before storing the host.
- If tracker is nil, methods are no-ops.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/clients ./internal/proxy -v`  
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/clients internal/proxy
git commit -m "feat: track proxy clients independently of capture"
```

---

### Task 7: API hosts endpoints and live application

**Files:**
- Create: `internal/api/hosts.go`
- Modify: `internal/api/server.go`
- Modify: `internal/api/config_http.go`
- Test: `internal/api/hosts_test.go`
- Modify: `internal/api/config_test.go` if earlier changes did not already add `hosts_profile`

**Interfaces:**
- Consumes: `hosts.Profiles`, `hosts.Table`, `hosts.ParseError`, `config.Session.SetHostsProfile`, `proxy.SetHosts`.
- Produces:
  ```go
  // api.Server
  func (s *Server) SetHostsProfiles(p *hosts.Profiles)
  func (s *Server) InitHosts() error

  // JSON
  type hostWarningJSON = hosts.Warning // already tagged
  type hostProfileJSON struct {
      Name     string          `json:"name"`
      Rules    int             `json:"rules"`
      Warnings []hosts.Warning `json:"warnings"`
      Error    string          `json:"error"`
  }
  type hostsListJSON struct {
      Active      string            `json:"active"`
      ActiveError string            `json:"active_error"`
      Profiles    []hostProfileJSON `json:"profiles"`
  }
  type hostDetailJSON struct {
      Name    string `json:"name"`
      Content string `json:"content"`
  }
  type hostSaveJSON struct {
      Name     string          `json:"name"`
      Rules    int             `json:"rules"`
      Warnings []hosts.Warning `json:"warnings"`
  }
  ```

  Routes:
  ```
  GET    /api/hosts                 200 hostsListJSON
  GET    /api/hosts/{name}          200 hostDetailJSON; missing 404 配置不存在
  PUT    /api/hosts/{name}          {content} → 200 hostSaveJSON
  POST   /api/hosts                 {name} → 204
  PUT    /api/hosts                 {active} → 200 hostsListJSON
  POST   /api/hosts/{name}/rename   {name} (new name) → 204
  DELETE /api/hosts/{name}          204; active 409
  ```

- [ ] **Step 1: Write failing API hosts tests**

Create `internal/api/hosts_test.go` with a helper that builds a session, profiles, proxy, and server:

```go
func newHostsServer(t *testing.T) (*api.Server, *httptest.Server, *config.Dir, *hosts.Profiles, *proxy.Proxy) {
    dir, file, err := config.Open(t.TempDir())
    if err != nil { t.Fatal(err) }
    session := config.NewSession(dir, file, config.Overrides{}, file.Proxy, file.UI, file.MaxFlows, file.MaxBody)
    profiles, err := hosts.OpenProfiles(dir.Path())
    if err != nil { t.Fatal(err) }
    ca, err := cert.LoadOrCreate(t.TempDir())
    if err != nil { t.Fatal(err) }
    px := proxy.New("127.0.0.1:0", nil, file.MaxBody, ca)
    srv := api.New(store.New(file.MaxFlows), ca, session, px)
    srv.SetHostsProfiles(profiles)
    if err := srv.InitHosts(); err != nil { t.Fatal(err) }
    ts := httptest.NewServer(srv.Handler())
    t.Cleanup(ts.Close)
    return srv, ts, dir, profiles, px
}
```

Tests:

- `TestHostsCRUD`: `POST /api/hosts {"name":"测试 环境"}` → 204; `GET /api/hosts` shows it with `rules:0`; `PUT /api/hosts/测试%20环境 {"content":"10.0.0.5 api.test\n10.0.0.5 api.test\n"}` → 200, `rules:1`, one warning; `GET` content; `POST .../rename {"name":"预发"}` → 204; `DELETE /api/hosts/预发` → 204; missing → 404.
- `TestHostsValidationErrors`: invalid name → 400 `配置名不合法`; duplicate create → 409 `同名配置已存在`; invalid content → 400 with `error` starting `第 1 行：` and JSON `line:1`; deleting active → 409 `当前启用的 Hosts 配置不能删除`; `PUT /api/hosts {"active":"missing"}` → 400 `配置不存在`.
- `TestActivateHostsWritesConfig`: create/save `测试`; `PUT /api/hosts {"active":"测试"}` → 200 and the body is a `hostsListJSON` with active `测试`; read `config.yaml` contains `hosts_profile: "测试"`; `GET /api/hosts` active is `测试`; `PUT /api/hosts {"active":""}` clears it.
- `TestPutConfigRejectsBadHostsProfile`: `PUT /api/config` with `hosts_profile:"missing"` → 400 `配置不存在`; config file unchanged.
- `TestInitHostsMissingProfile`: build a session whose `Values.HostsProfile` is `"missing"`, create the server, call `InitHosts`, expect an error mentioning the profile; the proxy must have an empty hosts table.
- `TestHostsRefreshOnList`: activate `测试`; overwrite the file on disk with two mappings; `GET /api/hosts` → rules 2; overwrite with `bad`; `GET /api/hosts` → `active_error` non-empty. (The last-good-rules assertion lives in Task 8's overview test.)

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/api -run 'TestHosts|TestActivateHosts|TestPutConfigRejects' -v`  
Expected: FAIL because routes and methods do not exist.

- [ ] **Step 3: Implement `internal/api/hosts.go`**

- Server fields: `hostsProfiles *hosts.Profiles`; `hostsMu sync.Mutex`; `hostsActive string`; `hostsRules int`; `hostsErr string`; `hostsModTime time.Time`; `hostsSize int64`.
- `SetHostsProfiles`, `InitHosts` (read session file `HostsProfile`, load, apply; return wrapped error).
- `loadHostsTable(name)` returns an empty table for `""`, `hosts.ErrNotFound` for missing, and validates the name.
- `applyHostsTable(name, table)`: update `hostsActive`/`hostsRules`/`hostsErr` under `hostsMu`, then call `s.proxy.SetHosts(name, table)` after unlocking (no lock inversion).
- `refreshActiveHosts()`: stat the active file; if unchanged return; read+parse; on error set `hostsErr` and keep the table; on success apply and clear the error. Missing active file sets `hostsErr = "文件不存在"`.
- `hostsList()`: list names from profiles; read+parse each; build entries; if active is not in the list, append an entry with the active error.
- `parseError` mapping to Chinese:
  `invalid IP` → `IP 不合法`, `invalid hostname` → `域名不合法`, `missing field` → `缺少字段`, `file exceeds 1 MiB` → `文件超过 1 MiB`, `too many mappings` → `规则超过 1000 条`; prefix `第 N 行：` when `Line > 0`. Return JSON with `line`.
- Handlers as specified above. `PUT /api/hosts` validates the target before `session.SetHostsProfile`, then applies. Rename active: rename file, update config; on config failure rename back.
- `PUT /api/config`: before `SaveAndApply`, if `next.HostsProfile != current` read/parse it and return 400 on failure; after a successful save apply the table (without saving again).
- Keep `applySaved` for max flows/body/capture only.
- Register routes in `server.go`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/api -run 'TestHosts|TestActivateHosts|TestPutConfigRejects' -v`  
Expected: PASS.

- [ ] **Step 5: Run the full API suite**

Run: `go test ./internal/api -v`  
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/api
git commit -m "feat: add hosts profile API and live activation"
```

---

### Task 8: Overview, capture rules, store count

**Files:**
- Modify: `internal/store/store.go`
- Test: `internal/store/store_test.go`
- Modify: `internal/api/server.go`
- Create: `internal/api/overview.go`
- Create: `internal/api/capture_rules.go`
- Test: `internal/api/overview_test.go`
- Test: `internal/api/capture_rules_test.go`

**Interfaces:**
- Consumes: `clients.Tracker`, `hosts` state, `store.Count`, `config.Session`.
- Produces:
  ```go
  func (s *Store) Count() int

  // api.Server
  func (s *Server) SetClients(t *clients.Tracker)

  // GET /api/overview
  type overviewJSON struct {
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

  // PUT /api/capture/rules
  type captureRulesBody struct {
      CaptureEmpty string   `json:"capture_empty"`
      CaptureHTTPS string   `json:"capture_https"`
      CaptureHosts []string `json:"capture_hosts"`
  }
  ```

- [ ] **Step 1: Write failing store/API tests**

`internal/store/store_test.go`:

```go
func TestCount(t *testing.T) {
    s := New(2)
    if s.Count() != 0 { t.Fatal(s.Count()) }
    s.Add(flow.Flow{ID: "1"})
    s.Add(flow.Flow{ID: "2"})
    s.Add(flow.Flow{ID: "3"})
    if s.Count() != 2 { t.Fatalf("Count()=%d", s.Count()) }
    s.Clear()
    if s.Count() != 0 { t.Fatal(s.Count()) }
}
```

`internal/api/overview_test.go` uses the Task 7 helper, adds two flows and a tracker request, activates a profile, and asserts every JSON field. `TestOverviewRefreshesHosts` activates a two-rule profile, overwrites the file with invalid content, calls overview, and asserts `hosts_error` is non-empty while `hosts_rules` stays 2 (the last good table).

`internal/api/capture_rules_test.go`:

- `PUT /api/capture/rules` with `{"capture_empty":"none","capture_https":"tunnel","capture_hosts":["example.com"]}` → 200, file contains the new values, `hosts_profile` unchanged, proxy policy updated (reflect the `policy` fields as `TestPutCapture` does).
- Missing/unknown/wrong-type fields → 400 with the existing config messages.
- Invalid host rule → 400 `主机规则不合法`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/store ./internal/api -run 'TestCount|TestOverview|TestCaptureRules' -v`  
Expected: FAIL because handlers do not exist.

- [ ] **Step 3: Implement**

- `Store.Count` under `RLock`.
- `api.New`: record `startedAt time.Time`; add `clients *clients.Tracker` and `SetClients`.
- `GET /api/overview`: call `refreshActiveHosts`; get `file, runtime := session.Snapshot()`; read hosts state; `s.clients.Snapshot()` (empty slice if nil); `s.store.Count()`; write JSON. `hosts_profile`/`hosts_rules` from the server's applied state; `hosts_error` from `hostsErr`.
- `PUT /api/capture/rules`: strict body decode (exactly three keys, all present); copy `file`; set the three fields; trim each `capture_hosts` entry and drop empty entries; `capture.Compile` to validate; `session.SaveAndApply(next, s.applySaved)`; return `{file, runtime}`. Map capture errors with `writeConfigError`.
- Register `GET /api/overview` and `PUT /api/capture/rules` in `server.go`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/store ./internal/api -v`  
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store internal/api
git commit -m "feat: expose overview and capture rule updates"
```

---

### Task 9: Startup wiring and hosts directory

**Files:**
- Modify: `cmd/httpproxy/main.go`
- Test: `go test ./...` (no new unit test; integration is covered by API/proxy tests)
- Modify: `internal/ui/embed_test.go` only if the `下载 CA` literal moves out of the script (it should remain in the home pane, so no change).

**Interfaces:**
- Consumes: `hosts.OpenProfiles`, `clients.New`, `api.Server.SetHostsProfiles`, `InitHosts`, `SetClients`, `proxy.SetClientTracker`.
- Produces: a process that creates `.httpproxy/hosts/`, loads the active profile before listening, and exits 1 on a bad active profile.

- [ ] **Step 1: Implement main wiring**

After `config.Open` and `cert.LoadOrCreate`:

```go
profiles, err := hosts.OpenProfiles(dir.Path())
if err != nil {
    fmt.Fprintf(os.Stderr, "hosts: %v\n", err)
    return 1
}
tracker := clients.New()
```

After `apiServer.SetProxy(proxyServer)`:

```go
apiServer.SetHostsProfiles(profiles)
apiServer.SetClients(tracker)
proxyServer.SetClientTracker(tracker)
if err := apiServer.InitHosts(); err != nil {
    fmt.Fprintf(os.Stderr, "hosts: %v\n", err)
    return 1
}
```

`InitHosts` must run before either listener starts, so a bad `hosts_profile` prevents the process from serving with silent fallback.

- [ ] **Step 2: Build and run the suite**

Run: `go test ./...`  
Expected: PASS.

- [ ] **Step 3: Smoke-start the binary**

Run: `go build -o bin/httpproxy ./cmd/httpproxy && timeout 3 ./bin/httpproxy /tmp/httpproxy-smoke || true`  
Expected: creates `/tmp/httpproxy-smoke/.httpproxy/hosts/`, logs the proxy/UI addresses, exits on timeout without a startup error. Remove `/tmp/httpproxy-smoke` afterward.

- [ ] **Step 4: Commit**

```bash
git add cmd/httpproxy internal/api
git commit -m "feat: load the active hosts profile at startup"
```

---

### Task 10: Frontend API types, helpers, and tests

**Files:**
- Modify: `web/src/types.ts`
- Modify: `web/src/api.ts`
- Create: `web/src/overview.ts`
- Create: `web/overview.test.ts`
- Modify: `web/src/capture.ts`
- Modify: `web/capture.test.ts`
- Modify: `web/package.json`

**Interfaces:**
- Consumes: the Task 8/9 API shapes.
- Produces:
  ```ts
  // types.ts
  export type View = 'home' | 'capture' | 'hosts' | 'settings'
  export interface ConfigValues { /* existing + */ hosts_profile: string }
  export interface ClientInfo { ip: string; active_conns: number; requests: number; tunnels: number; last_seen: string; hosts: string[] }
  export interface Overview { proxy: string; ui: string; started_at: string; uptime_seconds: number; capture: boolean; flow_count: number; hosts_profile: string; hosts_rules: number; hosts_error: string; clients: ClientInfo[] }
  export interface HostWarning { domain: string; ignored_line: number; first_line: number }
  export interface HostProfileInfo { name: string; rules: number; warnings: HostWarning[]; error: string }
  export interface HostsList { active: string; active_error: string; profiles: HostProfileInfo[] }
  export interface HostDetail { name: string; content: string }
  export interface HostSaveResult { name: string; rules: number; warnings: HostWarning[] }

  // api.ts
  export function getOverview(): Promise<Overview>
  export function listHosts(): Promise<HostsList>
  export function getHost(name: string): Promise<HostDetail>
  export function saveHost(name: string, content: string): Promise<HostSaveResult>
  export function createHost(name: string): Promise<void>
  export function renameHost(from: string, to: string): Promise<void>
  export function deleteHost(name: string): Promise<void>
  export function activateHost(name: string): Promise<HostsList>
  export function putCaptureRules(rules: CaptureRules): Promise<ConfigSnapshot>

  // overview.ts
  export function formatUptime(seconds: number): string
  export function formatAgo(iso: string, now?: number): string
  export function hostsSummary(hosts: string[], max?: number): { text: string; title: string }
  export function warningText(w: HostWarning): string

  // capture.ts
  export interface CaptureRules { capture_empty: 'all' | 'none'; capture_https: 'decrypt' | 'tunnel'; capture_hosts: string[] }
  export function captureRulesPayload(empty: 'all' | 'none', httpsMode: 'decrypt' | 'tunnel', hostsText: string): CaptureRules
  ```

- [ ] **Step 1: Write failing pure-function tests**

Create `web/overview.test.ts`:

```ts
describe('formatUptime', () => {
  it('formats seconds, minutes, hours, days', () => {
    assert.equal(formatUptime(0), '刚刚')
    assert.equal(formatUptime(59), '59 秒')
    assert.equal(formatUptime(90), '1 分钟')
    assert.equal(formatUptime(3700), '1 小时 1 分钟')
    assert.equal(formatUptime(90000), '1 天 1 小时')
  })
})

describe('formatAgo', () => {
  const now = Date.parse('2026-11-22T00:00:00Z')
  it('formats relative times', () => {
    assert.equal(formatAgo('2026-11-21T23:59:58Z', now), '2 秒前')
    assert.equal(formatAgo('2026-11-21T23:00:00Z', now), '1 小时前')
    assert.equal(formatAgo('bad', now), '—')
  })
})

describe('hostsSummary', () => {
  it('lists three and counts the rest', () => {
    assert.deepEqual(hostsSummary(['a', 'b', 'c', 'd']), { text: 'a、b、c 等 4 个', title: 'a、b、c、d' })
    assert.deepEqual(hostsSummary([]), { text: '—', title: '' })
  })
})

describe('warningText', () => {
  it('names both lines', () => {
    assert.equal(warningText({ domain: 'a', ignored_line: 5, first_line: 3 }), '第 5 行与第 3 行重复，已按第 3 行生效')
  })
})
```

Add a `captureRulesPayload` test:

```ts
const got = captureRulesPayload('none', 'tunnel', ' Example.com \n\n *.example.com \n')
assert.deepEqual(got, { capture_empty: 'none', capture_https: 'tunnel', capture_hosts: ['Example.com', '*.example.com'] })
```

Update the `ConfigValues` fixture in `web/capture.test.ts` with `hosts_profile: ''`.

Add `overview.test.ts` to the `test` script in `web/package.json`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd web && npm test`  
Expected: FAIL because `overview.ts`/`captureRulesPayload` do not exist.

- [ ] **Step 3: Implement types, API functions, and helpers**

- Add all types above; `ConfigValues` gets `hosts_profile: string`.
- Add API functions using `encodeURIComponent(name)` for path segments; reuse a local `readError(res, fallback)` helper for non-2xx bodies.
- `getOverview`, `listHosts`, `getHost`, `saveHost`, `createHost`, `renameHost`, `deleteHost`, `activateHost`, `putCaptureRules`.
- `formatUptime`, `formatAgo`, `hostsSummary`, `warningText` exactly as tested.
- `captureRulesPayload` trims/non-empty filters like the existing `capturePayload`. Keep `capturePayload` until Task 12 removes it.

- [ ] **Step 4: Run tests and the type build**

Run: `cd web && npm test && npm run build`  
Expected: PASS; Vite writes `internal/ui/dist`.

- [ ] **Step 5: Commit**

```bash
git add web/src web/overview.test.ts web/package.json web/package-lock.json internal/ui/dist
git commit -m "feat: add web API types and overview helpers"
```

---

### Task 11: Activity bar, app shell, home pane

**Files:**
- Modify: `web/src/components/ActivityBar.tsx`
- Create: `web/src/components/HomePane.tsx`
- Modify: `web/src/App.tsx`
- Delete: `web/src/components/TopBar.tsx`
- Modify: `web/src/styles.css` (home/activity/topbar rules)
- Test: `cd web && npm test && npm run build` plus manual checks.

**Interfaces:**
- Consumes: `Overview`, `getOverview`, `listHosts`, `activateHost`, `getConfig`, `captureActionLabel`, `rulesMark`, `formatUptime`, `formatAgo`, `hostsSummary`.
- Produces:
  ```tsx
  // ActivityBar
  interface Props { view: View; onSelect: (view: View) => void }

  // HomePane
  interface Props {
    snapshot: ConfigSnapshot | null
    loadError: boolean
    captureBusy: boolean
    captureError: string
    onToggleCapture: () => void
    onOpenCapture: () => void
    onOpenHosts: () => void
    onConfigChanged: (snapshot: ConfigSnapshot) => void
  }

  // App
  const [view, setView] = useState<View>('home')
  ```

- [ ] **Step 1: Implement ActivityBar**

- Order: home, capture, hosts; spacer; settings.
- Inline SVGs: house (`M3 8l7-5 7 5v6a1 1 0 0 1-1 1h-3v-4H7v4H4a1 1 0 0 1-1-1z`), list/pulse, server/globe, existing gear.
- Each button: `aria-label`, `title`, `aria-pressed={view===...}`, class `activity-btn active` when selected. No `open` prop.

- [ ] **Step 2: Implement HomePane**

- Poll `getOverview()` every 2000 ms on mount; keep the last good data; show `无法读取代理状态` on failure after a successful load.
- Load `listHosts()` on mount and after activation for the dropdown.
- Header: `httpproxy`, proxy address with a copy button (`navigator.clipboard.writeText('http://' + proxy)`; show `已复制` for 1.5 s), `已运行 {formatUptime(uptime_seconds)}`.
- Rows:
  - 抓包: dot, `已开启/已关闭`, rule summary from `snapshot.file` (`rulesMark`), `{flow_count} 条记录`, button `captureActionLabel(capture)` disabled when `captureBusy || snapshot == null`, `查看抓包` → `onOpenCapture`.
  - Hosts: active name + rules or `未配置`; `<select>` with all profile names + `不启用`; on change call `activateHost`, then `getConfig()` and `onConfigChanged`, then refresh the list; on error revert selection and show the message; `管理` → `onOpenHosts`; show `hosts_error` as a warning row.
  - CA: `<a className="btn" href="/api/ca.crt" download="httpproxy-ca.crt">下载 CA</a>`; when `capture === false`, note `抓包关闭时不需要安装证书`.
- Clients section: table with `连接`, `请求`, `HTTPS`, `最近活动`, `最近访问`; rows sorted by the server; `formatAgo(last_seen)`, `hostsSummary(hosts)`. Empty state includes the proxy address.
- Reuse existing `.notice` for errors.

- [ ] **Step 3: Rewire App**

- `view` state default `home`.
- Render `ActivityBar` inside `.shell`.
- Render `HomePane` when `view === 'home'`, passing `onConfigChanged={onSaved}` (the same handler that updates theme/limits).
- Keep the capture JSX inline for now but add a temporary toolbar with count, toggle, and clear (the next task extracts it). Add the paused banner when `snapshot.file.capture === false`.
- Render `SettingsPane` when `view === 'settings'` with `open={true}` (SettingsPane still has the prop until Task 12).
- Remove the `TopBar` import and JSX; delete `TopBar.tsx`.
- Add a temporary hosts placeholder `<div className="settings"><h1>Hosts</h1></div>` for `view === 'hosts'`; Task 13 replaces it.
- `onSaved` remains shared; `onToggle` now used by HomePane and the capture toolbar.

- [ ] **Step 4: Add home/activity CSS**

In `styles.css`:
- Delete `.topbar*` rules.
- `.home` full-height scroll container; `.home-head`, `.home-row`, `.home-label`, `.home-hint`, `.home-actions`, `.home-section`, `.home-section h2`.
- `.client-table` with the same header/cell treatment as `.flow-table`; `.client-hosts` for the hosts cell; wrap the table in a horizontally scrollable `.clients-wrap`.
- Keep `.activity` and `.activity-btn`; the active state already works.
- Use only existing variables.

- [ ] **Step 5: Run tests and build**

Run: `cd web && npm test && npm run build`  
Expected: PASS. The embedded UI still contains `下载 CA` (now from HomePane).

- [ ] **Step 6: Manual check**

Run `go run ./cmd/httpproxy` and open the UI: default home, proxy address/copy, capture status, CA link, client table (empty state or 127.0.0.1 after a request), settings gear still works, capture/settings panels switch.

- [ ] **Step 7: Commit**

```bash
git add web/src internal/ui/dist
git commit -m "feat: add activity bar and home pane"
```

---

### Task 12: Capture pane, rules editor, settings cleanup

**Files:**
- Create: `web/src/components/CapturePane.tsx`
- Create: `web/src/components/CaptureRules.tsx`
- Modify: `web/src/App.tsx`
- Modify: `web/src/components/SettingsPane.tsx`
- Modify: `web/src/capture.ts`
- Modify: `web/capture.test.ts`
- Modify: `web/src/styles.css`
- Test: `cd web && npm test && npm run build`.

**Interfaces:**
- Consumes: `captureActionLabel`, `rulesMark`, `captureRulesPayload`, `putCaptureRules`, `putCapture` (via App), `FlowSummary`, `Flow`, `Filters`.
- Produces:
  ```tsx
  // CapturePane
  interface Props {
    flows: FlowSummary[]
    selectedId: string | null
    onSelect: (id: string) => void
    filters: Filters
    onFilters: (filters: Filters) => void
    visible: string[]
    onVisible: (ids: string[]) => void
    detail: Flow | null
    loading: boolean
    dock: 'bottom' | 'right'
    onToggleDock: () => void
    snapshot: ConfigSnapshot | null
    loadError: boolean
    onClear: () => void
    onToggleCapture: () => void
    captureBusy: boolean
    captureError: string
    onSaved: (snapshot: ConfigSnapshot) => void
  }

  // SettingsPane (remove capture fields and the open prop)
  interface Props {
    snapshot: ConfigSnapshot | null
    loadError: boolean
    onSaved: (snapshot: ConfigSnapshot) => void
    writeLocked: boolean
    beginWrite: () => boolean
    endWrite: () => void
  }
  ```

- [ ] **Step 1: Create CapturePane**

Move the entire capture JSX from `App.tsx` into `CapturePane`. Add a toolbar above the filter bar:

- Left: capture toggle button (`captureActionLabel`), `{flows.length} 条请求`, `清空`.
- Right: `规则` button; `RulesMark` stays in the filter bar.
- When `capture === false`, render a `.notice` below the toolbar: `抓包已关闭，HTTPS 以隧道转发，不需要 CA。已有记录保留。`
- Toolbar errors use `.topbar-error` (rename it `.toolbar-error` if preferred; keep styles).

- [ ] **Step 2: Create CaptureRules**

A small popover form:

- Local state initialized from `snapshot.file`: `capture_empty`, `capture_https`, `capture_hosts` text.
- Three controls: 空规则, 未抓 HTTPS, 抓包域名 textarea.
- Save calls `putCaptureRules(captureRulesPayload(...))`; on success `onSaved(next)`, close, clear error; on failure show the message.
- Note when `capture === false`: `抓包关闭时以上规则不生效`.
- Close on `Escape`/outside click is optional; a `关闭` button is enough.

- [ ] **Step 3: Clean up SettingsPane and App**

- Remove capture fields (`empty`, `httpsMode`, `hostsText`), their effects, rows, and imports.
- Save payload: `{...snapshot.file, proxy, ui, max_flows: Number(maxFlows), max_body: Number(maxBody), theme}`.
- Remove the `open` prop and the `hidden` wrapper; render only when selected.
- App passes `CapturePane` for `view === 'capture'`; remove the inline capture JSX and the temporary toolbar.
- Remove `capturePayload` from `capture.ts` and its test; keep `captureRulesPayload`.

- [ ] **Step 4: Add toolbar/rules CSS**

Add `.capture-toolbar`, `.toolbar-error`, `.rules-editor`, `.rules-editor .settings-row` styles. Reuse `.notice`, `.input`, `.btn`, `.seg-btn`.

- [ ] **Step 5: Run tests and build**

Run: `cd web && npm test && npm run build`  
Expected: PASS.

- [ ] **Step 6: Manual check**

Open capture: toggle pauses/ resumes, clear works, rules save persists and updates the summary; settings no longer shows capture rules; capture off shows the tunnel notice.

- [ ] **Step 7: Commit**

```bash
git add web/src web/capture.test.ts internal/ui/dist
git commit -m "feat: move capture controls and rules into the capture pane"
```

---

### Task 13: Hosts pane

**Files:**
- Create: `web/src/components/HostsPane.tsx`
- Modify: `web/src/App.tsx`
- Modify: `web/src/styles.css`
- Test: `cd web && npm test && npm run build` plus manual checks.

**Interfaces:**
- Consumes: `listHosts`, `getHost`, `saveHost`, `createHost`, `renameHost`, `deleteHost`, `activateHost`, `getConfig`, `warningText`, `ConfigSnapshot`.
- Produces:
  ```tsx
  interface Props {
    writeLocked: boolean
    beginWrite: () => boolean
    endWrite: () => void
    onConfigChanged: (snapshot: ConfigSnapshot) => void
  }
  ```

- [ ] **Step 1: Implement the list/editor layout**

```text
Hosts                                              [新建配置]
┌ 配置 ───────────────┬ 测试环境   ● 当前启用  [停用] [重命名] [删除]
│ ● 测试环境   2 条   │ textarea
│   预发       5 条   │
│                     │ 2 条规则 · 1 行重复已忽略        [保存]
└─────────────────────┴────────────────────────────────────────────
```

- Fetch `listHosts()` on mount; if active, fetch its content.
- Profile row: name, rule count, active marker; click selects for editing. `启用` button for inactive; `停用` for active; `重命名`/`删除` for the selected profile.
- `删除` disabled when the selected profile is active.
- `新建` shows an inline input row; confirm creates and selects, does not activate.
- Rename turns the header name into an input; confirm calls `renameHost`.
- Dirty state: `content !== savedContent`; switching profiles with dirty content uses `window.confirm('未保存的修改会丢失，继续？')`.
- Save: `saveHost(name, content)`; show `已保存` for 1.5 s; update warnings/rules; if active, refresh config via `getConfig()` and `onConfigChanged`.
- Activate/deactivate/rename-active: wrap in `beginWrite`/`endWrite`; after success refresh list + `getConfig` + `onConfigChanged`.
- Error display: `hostsList.active_error` banner `当前启用的 Hosts 配置有误，仍在使用上一次有效的规则`; per-profile red `有错误`; editor error message from the API (already includes line).
- Warnings: `warnings.length` → `M 行重复已忽略`, expandable list of the first 3 `warningText` entries.
- Empty state: `还没有 Hosts 配置` + new button.

- [ ] **Step 2: Wire into App**

- Replace the placeholder for `view === 'hosts'` with `<HostsPane writeLocked={writeLocked} beginWrite={beginWrite} endWrite={endWrite} onConfigChanged={onSaved} />`; reuse the same `onSaved` handler so theme/limits stay in sync.

- [ ] **Step 3: Add CSS**

- `.hosts`, `.hosts-list`, `.hosts-list-head`, `.hosts-item`, `.hosts-item.active`, `.hosts-item-name`, `.hosts-item-count`, `.hosts-main`, `.hosts-head`, `.hosts-textarea`, `.hosts-status`, `.hosts-warnings`, `.hosts-empty`.
- Textarea uses `font-family: var(--mono)`, min-height 240px, flex 1.
- At narrow widths the list shrinks to 160px; the editor keeps a minimum width.

- [ ] **Step 4: Run tests and build**

Run: `cd web && npm test && npm run build`  
Expected: PASS.

- [ ] **Step 5: Manual check**

Create two profiles, assign different IPs, activate each, verify a curl through the proxy changes upstream; edit the active file on disk, revisit the pane, see rules/errors update; delete/rename; dirty-switch confirmation; duplicate lines show the warning.

- [ ] **Step 6: Commit**

```bash
git add web/src internal/ui/dist
git commit -m "feat: add hosts profile pane"
```

---

### Task 14: Flow detail upstream section and final styles

**Files:**
- Modify: `web/src/types.ts` (`Flow.upstream_addr?`, `Flow.hosts_profile?`)
- Modify: `web/src/components/FlowDetail.tsx`
- Modify: `web/src/styles.css`
- Modify: `web/src/components/TopBar.tsx` deletion already done; confirm no `.topbar` references.
- Test: `cd web && npm test && npm run build`.

**Interfaces:**
- Consumes: `flow.upstream_addr`, `flow.hosts_profile`.
- Produces: a Headers section `上游` shown only when `upstream_addr` is non-empty.

- [ ] **Step 1: Add the type fields and detail section**

```tsx
{flow.upstream_addr ? (
  <section className="hdr-section">
    <h4>上游</h4>
    <dl className="hdr-list">
      <div className="hdr-row"><dt>目标：</dt><dd>{`${flow.scheme}://${flow.host}`}</dd></div>
      <div className="hdr-row"><dt>实际拨号：</dt><dd>{flow.upstream_addr}</dd></div>
      <div className="hdr-row"><dt>Hosts 配置：</dt><dd>{flow.hosts_profile}</dd></div>
    </dl>
  </section>
) : null}
```

Place it after `常规` and before `查询参数`.

- [ ] **Step 2: Finish CSS cleanup**

- Remove any remaining `.topbar` selectors and unused `TopBar` imports.
- Verify `.activity`, `.home`, `.client-table`, `.hosts*`, `.capture-toolbar`, `.rules-editor` all use existing variables.
- Add focus-visible styles for the new buttons/inputs consistent with `.btn`/`.input`.

- [ ] **Step 3: Run tests and build**

Run: `cd web && npm test && npm run build`  
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add web/src internal/ui/dist
git commit -m "feat: show hosts routing in flow details"
```

---

### Task 15: README and end-to-end verification

**Files:**
- Modify: `README.md`
- Modify: `internal/ui/embed_test.go` only if its `下载 CA` assertion needs a different source (it should still pass).
- Test: `make test`, `make build`, manual checklist.
- Commit: `internal/ui/dist` rebuilt artifacts.

- [ ] **Step 1: Update README**

- Opening: local HTTP/HTTPS proxy with optional capture and per-profile hosts overrides; still does not modify requests/responses.
- Defaults: `capture: false`, `hosts_profile: ""`.
- New behavior: capture off = HTTPS tunnel, no CA needed; capture on = existing MITM + rules; pause stops new records immediately.
- Layout: four panels, home default; settings keeps theme/addresses/limits.
- Hosts: `.httpproxy/hosts/<name>.hosts`, format example, first duplicate wins, active in `config.yaml`, immediate effect on new connections.
- CA instructions: only needed when capture is on.
- Limitations: no wildcard/port mapping, no process attribution, client stats in memory.
- Keep the existing build/run/CLI sections.

- [ ] **Step 2: Run the full automated suite**

Run:

```bash
make test
make build
```

Expected: Go tests, web tests, TypeScript build, Vite build, and `go build` all pass.

- [ ] **Step 3: Manual end-to-end checklist**

1. Start `./bin/httpproxy` in a scratch workdir. Home opens by default with capture off.
2. `curl -x http://127.0.0.1:8080 https://example.com` succeeds without installing the CA.
3. Create `测试.hosts` with `127.0.0.1 api.test`; activate it; run a local HTTP server on an unprivileged port and confirm `curl -x http://127.0.0.1:8080 http://api.test:<port>/` hits it (the port comes from the request, not the hosts file). For HTTPS, point a local TLS server at `127.0.0.1:<port>` with a certificate for `api.test` and confirm the upstream sees SNI `api.test`.
4. Switch to a second profile pointing at another local server; confirm the next request goes to the new IP.
5. Turn capture on; install/trust the CA; confirm HTTPS requests appear; pause capture; confirm new requests stop appearing while traffic continues; resume.
6. Open a second machine on the LAN with the proxy configured; confirm a second client row appears (requires `-proxy 0.0.0.0:8080 -ui 0.0.0.0:8081`).
7. External-edit the active hosts file to invalid content; revisit the Hosts pane; confirm the old rules still work and the warning appears.
8. Check the settings page no longer shows capture rules; capture rules are in the capture pane.

- [ ] **Step 4: Review the final diff**

Run: `git status --short && git diff --stat HEAD`  
Expected: only the intended source, docs, README, and `internal/ui/dist` changes. Do not commit unrelated worktree or `.httpproxy` files.

- [ ] **Step 5: Commit**

```bash
git add README.md web/src web/*.test.ts web/package.json web/package-lock.json internal/ui/dist
git commit -m "docs: describe proxy-first panels and hosts overrides"
```

If `make build` changed `internal/ui/dist`, include those artifacts in the commit; they are part of the single-binary build.
