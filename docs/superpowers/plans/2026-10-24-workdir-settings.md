# Workdir And Settings Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Start `httpproxy` against an existing directory, store config and the CA in `<DIR>/.httpproxy/`, and edit that file from a settings page opened by a gear at the bottom of a 32px activity bar.

**Architecture:** `internal/config` parses the YAML file, creates the private directory, and remembers which CLI flags overrode this process. `internal/api` serves `file` and `runtime` snapshots and is the only writer. Listen addresses stay on the sockets opened at startup. `max_flows` and `max_body` change the live process only when that flag was absent. Theme always follows the saved file. The React shell toggles between the existing capture view and one settings form.

**Tech Stack:** Go 1.24.4, `gopkg.in/yaml.v3` v3.0.1, existing `net/http` API, React 19, TypeScript, Vite, one hand-written `styles.css`. No new npm dependency and no router.

**Spec:** `docs/superpowers/specs/2026-10-24-workdir-settings-design.md`

## Global Constraints

- Command shape is `httpproxy [DIR] [-proxy ADDR] [-ui ADDR] [-max-flows N] [-max-body N]`. At most one `DIR`. It must already exist and be a directory. Omitted `DIR` means the process working directory. Resolve it with `filepath.Abs`. Do not `os.Chdir`. Do not expand `~` in Go.
- Flags may appear before or after `DIR`. A lone `--` ends flag parsing; everything after it is positional. Unknown flags, including `-ca-dir`, exit 2. A flag the `flag` package accepts but whose value is outside the allowed range also exits 2, and that path must not create `.httpproxy/`.
- Exit 2: bad flags, extra positionals, missing or non-directory `DIR`. Exit 1: cannot create `.httpproxy/`, cannot chmod it to `0700`, illegal config file, incomplete CA, listen failure, or `.httpproxy` exists and is not a directory. stderr is English.
- Layout is `<DIR>/.httpproxy/` at mode `0700`, containing `.gitignore` (`*\n`, create only when absent), `config.yaml` at `0644`, `ca.crt`, and `ca.key` at `0600`. Do not create or edit a `.gitignore` in the workdir root. Tighten an existing directory to `0700` before writing config and before returning from CA load. An illegal existing `config.yaml` is left byte-for-byte unchanged; this start may still chmod the directory and create a missing `.gitignore`.
- Default file, in this order: `proxy: 127.0.0.1:8080`, `ui: 127.0.0.1:8081`, `max_flows: 1000`, `max_body: 1048576`, `theme: dark`. All five keys are required. Addresses and theme are strings. Counts are integers, not strings or floats. Unknown keys are illegal. Theme is only `dark` or `light`. An address is legal when `net.SplitHostPort` succeeds, so `:8080` and `[::1]:8080` are legal. `max_flows` fits in a Go `int` and is `>= 1`. `max_body` fits in a Go `int64` and is `>= 0`.
- A flag counts as an override only when that name appears in argv. Do not compare it with the default. The first written default file does not contain override values. Theme has no flag.
- Saving writes the file and does not copy overrides back into it. Saved proxy and UI addresses never rebind; runtime addresses stay at the values chosen when the process opened its sockets. Without `-max-flows`, save updates the ring immediately and shrinking it emits the existing `flow.evicted` events. With `-max-flows`, the ring stays until restart. Without `-max-body`, flows created after the save use the new limit, and that limit is fixed for both bodies of that flow. With `-max-body`, the limit stays until restart. Theme updates `runtime.theme` and the open page immediately.
- `GET /api/config` and a successful `PUT /api/config` return `{ "file": {...}, "runtime": {...} }` with keys `proxy`, `ui`, `max_flows`, `max_body`, `theme`. PUT rejects a missing field, JSON `null`, an unknown field, and a non-integer count. Validation order is proxy, UI, max_flows, max_body, theme, then missing, then unknown. Return only the first error. Rewrite YAML in that same key order through a temp file in `.httpproxy/` and rename it over `config.yaml`. Drop comments and extra blank lines.
- API `error` strings are exactly: `代理地址必须是 host:port`, `UI 地址必须是 host:port`, `保留条数必须是大于等于 1 的整数`, `正文上限必须是大于等于 0 的整数`, `主题必须是 dark 或 light`, `配置缺少字段`, `配置含有未知字段`, `配置不是合法的 JSON`, `无法写入配置`. The first eight are HTTP 400. The last is HTTP 500. Startup stderr uses `invalid proxy address`, `invalid ui address`, `max_flows must be an integer >= 1`, `max_body must be an integer >= 0`, `theme must be dark or light`, `missing config field`, `unknown config field`, plus the OS error on write failure.
- The activity bar is 32px, `--activity` is `#010409` in dark and `#f6f8fa` in light, and the only control is a bottom gear labeled `设置`. An active gear has a 2px accent bar on its left. Click toggles settings and capture. Refresh returns to capture. The top bar drops the theme button and shows the runtime proxy address, the count, `下载 CA`, and `清空`. Clear and the WebSocket keep working on the settings page. The settings form has no category column. Column visibility stays on the filter bar under `httpproxy.columns`.
- The form edits `file` values. Address rows always show `当前` plus the runtime address and `重启后生效`. Count rows show `本次实际` only when the file number differs from runtime. A failed save keeps the draft and shows the API `error` beside the button. A failed initial GET shows `无法读取配置`, hides the form, and disables save. The button is disabled until a snapshot exists and while a request is in flight.
- Served `index.html` sets `<html lang="zh-CN" data-theme="...">` from `runtime.theme`. Delete the `httpproxy.theme` localStorage script from `web/index.html`. Vite starts dark, then applies `runtime.theme` after a successful GET. A failed GET leaves the already painted theme alone and the list cap at 1000.
- Do not add an in-app restart, a hot rebind, a settings category list, or flow persistence. Do not migrate `~/.httpproxy`. Do not add an npm package or a router. Keep the existing local Host allowlist.
- The worktree already has unrelated uncommitted edits. Do not revert them. `git add` only the paths named in the task. Never `git add -A`.

## File Structure

| Path | Responsibility |
| --- | --- |
| `internal/config/value.go` | `Values`, sentinels, `ParseYAML`, `ParseJSON`, `MarshalYAML`, `Defaults` |
| `internal/config/value_test.go` | Parser and validator tests |
| `internal/config/dir.go` | `Open`, `Save`, mode `0700`, gitignore, atomic replace |
| `internal/config/dir_test.go` | Directory lifecycle tests |
| `internal/config/session.go` | Startup effective values and save rules |
| `internal/config/session_test.go` | Override stickiness |
| `internal/store/store.go` | `SetMax` eviction |
| `internal/proxy/proxy.go` | `SetMaxBody` / `maxBodyLimit` |
| `internal/proxy/http1.go`, `internal/proxy/http2.go` | One limit per flow |
| `internal/api/server.go`, `internal/api/config_http.go` | `GET` and `PUT /api/config` |
| `cmd/httpproxy/args.go`, `cmd/httpproxy/main.go` | argv, exit codes, wiring |
| `internal/ui/embed.go` | Inject `data-theme` and strip the old theme script |
| `web/src/components/ActivityBar.tsx`, `web/src/components/SettingsPane.tsx` | Shell and form |
| `web/src/settings.ts` | Note text |
| `README.md` | New command and layout |

---

### Task 1: Config values

**Files:**
- Create: `internal/config/value.go`
- Create: `internal/config/value_test.go`
- Modify: `go.mod`
- Modify: `go.sum`

**Interfaces:**
- Consumes: nothing
- Produces:
  - `type Values struct { Proxy string; UI string; MaxFlows int; MaxBody int64; Theme string }` with json tags `proxy`, `ui`, `max_flows`, `max_body`, `theme` and the same yaml tags
  - `func Defaults() Values`
  - `var ErrProxyAddress, ErrUIAddress, ErrMaxFlows, ErrMaxBody, ErrTheme, ErrMissingField, ErrUnknownField, ErrJSON error`
  - `func ParseYAML(data []byte) (Values, error)`
  - `func ParseJSON(data []byte) (Values, error)`
  - `func MarshalYAML(v Values) []byte`
  - English `Error()` strings listed in Global Constraints

- [ ] **Step 1: Add the dependency**

Run: `go get gopkg.in/yaml.v3@v3.0.1`

Expected: `go.mod` requires `gopkg.in/yaml.v3 v3.0.1`.

- [ ] **Step 2: Write the failing test**

Create `internal/config/value_test.go`:

```go
package config

import (
	"errors"
	"strings"
	"testing"
)

func TestDefaultsAndRoundTrip(t *testing.T) {
	got, err := ParseYAML(MarshalYAML(Defaults()))
	if err != nil {
		t.Fatal(err)
	}
	if got != Defaults() {
		t.Fatalf("%+v", got)
	}
	text := string(MarshalYAML(Defaults()))
	for i, key := range []string{"proxy:", "ui:", "max_flows:", "max_body:", "theme:"} {
		if strings.Index(text, key) < 0 {
			t.Fatalf("missing %s", key)
		}
		if i > 0 && strings.Index(text, key) < strings.Index(text, []string{"proxy:", "ui:", "max_flows:", "max_body:", "theme:"}[i-1]) {
			t.Fatalf("order %s", text)
		}
	}
}

func TestParseYAMLAcceptsListenForms(t *testing.T) {
	raw := []byte("proxy: \":8080\"\nui: \"[::1]:8081\"\nmax_flows: 1\nmax_body: 0\ntheme: light\n")
	got, err := ParseYAML(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.Proxy != ":8080" || got.UI != "[::1]:8081" || got.MaxFlows != 1 || got.MaxBody != 0 || got.Theme != "light" {
		t.Fatalf("%+v", got)
	}
}

func TestParseYAMLRejectsShape(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want error
	}{
		{"float", "proxy: 127.0.0.1:8080\nui: 127.0.0.1:8081\nmax_flows: 1.5\nmax_body: 1\ntheme: dark\n", ErrMaxFlows},
		{"quoted count", "proxy: 127.0.0.1:8080\nui: 127.0.0.1:8081\nmax_flows: \"1\"\nmax_body: 1\ntheme: dark\n", ErrMaxFlows},
		{"unknown", "proxy: 127.0.0.1:8080\nui: 127.0.0.1:8081\nmax_flows: 1\nmax_body: 1\ntheme: dark\nextra: 1\n", ErrUnknownField},
		{"missing", "proxy: 127.0.0.1:8080\nui: 127.0.0.1:8081\nmax_flows: 1\nmax_body: 1\n", ErrMissingField},
		{"bad proxy", "proxy: nope\nui: 127.0.0.1:8081\nmax_flows: 1\nmax_body: 1\ntheme: dark\n", ErrProxyAddress},
		{"bad ui", "proxy: 127.0.0.1:8080\nui: nope\nmax_flows: 1\nmax_body: 1\ntheme: dark\n", ErrUIAddress},
		{"zero flows", "proxy: 127.0.0.1:8080\nui: 127.0.0.1:8081\nmax_flows: 0\nmax_body: 1\ntheme: dark\n", ErrMaxFlows},
		{"negative body", "proxy: 127.0.0.1:8080\nui: 127.0.0.1:8081\nmax_flows: 1\nmax_body: -1\ntheme: dark\n", ErrMaxBody},
		{"theme", "proxy: 127.0.0.1:8080\nui: 127.0.0.1:8081\nmax_flows: 1\nmax_body: 1\ntheme: blue\n", ErrTheme},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseYAML([]byte(tc.yaml))
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestParseJSON(t *testing.T) {
	raw := []byte(`{"proxy":"127.0.0.1:8080","ui":"127.0.0.1:8081","max_flows":2,"max_body":3,"theme":"dark"}`)
	got, err := ParseJSON(raw)
	if err != nil || got.MaxFlows != 2 || got.MaxBody != 3 {
		t.Fatalf("%+v %v", got, err)
	}
	for _, body := range [][]byte{
		[]byte(`{"proxy":"127.0.0.1:8080","ui":"127.0.0.1:8081","max_flows":1.5,"max_body":1,"theme":"dark"}`),
		[]byte(`not json`),
		[]byte(`{"proxy":null,"ui":"127.0.0.1:8081","max_flows":1,"max_body":1,"theme":"dark"}`),
		[]byte(`{"proxy":"127.0.0.1:8080","ui":"127.0.0.1:8081","max_flows":1,"max_body":1,"theme":"dark","x":1}`),
	} {
		if _, err := ParseJSON(body); err == nil {
			t.Fatalf("accepted %s", body)
		}
	}
	_, err := ParseJSON([]byte(`{"ui":"127.0.0.1:8081","max_flows":1,"max_body":1,"theme":"dark"}`))
	if !errors.Is(err, ErrMissingField) {
		t.Fatal(err)
	}
	_, err = ParseJSON([]byte(`{"proxy":null,"ui":"nope","max_flows":1,"max_body":1,"theme":"dark"}`))
	if !errors.Is(err, ErrUIAddress) {
		t.Fatalf("present UI error wins over a missing proxy, got %v", err)
	}
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test ./internal/config/ -count=1`

Expected: FAIL because package `config` does not exist.

- [ ] **Step 4: Implement the parser**

Create `internal/config/value.go` with `Defaults`, the sentinel errors, `ParseYAML`, `ParseJSON`, and `MarshalYAML`.

`ParseYAML` decodes a `yaml.Node`. Unwrap a document node. Require a mapping. Walk key/value pairs into a map. Remember unknown keys, but do not return yet. Then, for proxy, UI, `max_flows`, `max_body`, and theme in that order, validate a key only when it is present and is not YAML null. Address and theme nodes must be `!!str`. Count nodes must be `!!int`. `net.SplitHostPort` decides the address. `max_flows` uses `strconv.ParseInt(node.Value, 10, strconv.IntSize)` and must be `>= 1`. `max_body` uses bit size 64 and must be `>= 0`. Theme must be `dark` or `light`. After those five checks, a missing or null key returns `ErrMissingField`. An unknown key returns `ErrUnknownField`. A present illegal proxy therefore wins over a missing theme.

`ParseJSON` uses `json.NewDecoder`, `DisallowUnknownFields`, and a `map[string]json.RawMessage`. `DisallowUnknownFields` rejects unknown keys before the map exists, which would report `ErrJSON` instead of `ErrUnknownField`. Decode into `map[string]json.RawMessage` without that decoder flag, and reject a second JSON value with `ErrJSON`. A syntax error returns `ErrJSON`. Treat a missing key and a raw value equal to `null` as absent. Validate present fields in the same order as YAML, then missing, then unknown. Strings use `json.Unmarshal` into `string`. Counts reject raw text containing `.`, `e`, or `E`, then `strconv.ParseInt`.

`MarshalYAML` writes exactly:

```text
proxy: <yaml-quoted>
ui: <yaml-quoted>
max_flows: <decimal>
max_body: <decimal>
theme: <yaml-quoted>
```

Quote strings with `strconv.Quote`, which is a valid YAML double-quoted scalar.

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./internal/config/ -count=1`

Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/config/value.go internal/config/value_test.go go.mod go.sum
git commit -m "$(cat <<'EOF'
feat: parse httpproxy config values

EOF
)"
```

---

### Task 2: Private directory

**Files:**
- Create: `internal/config/dir.go`
- Create: `internal/config/dir_test.go`

**Interfaces:**
- Consumes: `Defaults`, `ParseYAML`, `MarshalYAML`, `Values`
- Produces:
  - `type Dir struct` with unexported path
  - `func (d *Dir) Path() string`
  - `func Open(workdir string) (*Dir, Values, error)`
  - `func (d *Dir) Save(v Values) error`
  - `func ChmodPrivate(path string) error`

- [ ] **Step 1: Write the failing test**

Create `internal/config/dir_test.go` covering:

- `Open(t.TempDir())` creates `<dir>/.httpproxy` at mode `0700`, writes `config.yaml` equal to `string(MarshalYAML(Defaults()))`, writes `.gitignore` equal to `*\n`, and `Path()` ends in `.httpproxy`.
- A second `Open` does not change an existing `.gitignore` whose contents are `keep\n`.
- A pre-created `.httpproxy` at `0755` is `0700` after `Open`.
- A file named `.httpproxy` makes `Open` return an error and leaves that file in place.
- `config.yaml` containing `nope: 1\n` makes `Open` return an error, and the file bytes are unchanged. The directory is still `0700`. A missing `.gitignore` now exists and is `*\n`.
- `Save` replaces `config.yaml` with `MarshalYAML` of a legal `Values`. A crash token is not required. After `Save`, `ParseYAML` of the file equals the saved value, and a sibling temp matching `.config-*.tmp` is gone.

Use `os.Stat` and `Mode().Perm()`.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/config/ -run 'TestOpen|TestSave' -count=1`

Expected: FAIL because `Open` is undefined.

- [ ] **Step 3: Implement Open and Save**

`Open` joins `filepath.Abs(workdir)` with `.httpproxy`. `Lstat`: if it exists and `!IsDir()`, return `fmt.Errorf("%s is not a directory", path)`. If it is missing, `os.Mkdir(path, 0o700)`. Then `os.Chmod(path, 0o700)`. If `.gitignore` is missing, `os.WriteFile` `*\n` at `0644`. If `config.yaml` is missing, `Save` the defaults and return them. If it exists, `os.ReadFile` and `ParseYAML`. On parse failure return that error and do not write `config.yaml`.

`Save` validates by `ParseYAML(MarshalYAML(v))` so a bad value cannot be written. Write `os.CreateTemp(dir, ".config-*.tmp")`, `chmod 0644`, write the marshal bytes, close, then `os.Rename` onto `config.yaml`. On any error after create, `os.Remove` the temp file.

`ChmodPrivate` is `os.Chmod(path, 0o700)`.

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/config/ -count=1`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/config/dir.go internal/config/dir_test.go
git commit -m "$(cat <<'EOF'
feat: store httpproxy config in .httpproxy

EOF
)"
```

---

### Task 3: Runtime session

**Files:**
- Create: `internal/config/session.go`
- Create: `internal/config/session_test.go`

**Interfaces:**
- Consumes: `Dir`, `Values`, `ParseYAML`
- Produces:
  - `type Overrides struct { Proxy, UI, MaxFlows, MaxBody bool }`
  - `type Applied struct { File, Runtime Values; SetMaxFlows bool; MaxFlows int; SetMaxBody bool; MaxBody int64 }`
  - `func Effective(file Values, proxy string, ui string, maxFlows int, maxBody int64, ov Overrides) Values`
  - `func NewSession(dir *Dir, file Values, ov Overrides, proxy string, ui string, maxFlows int, maxBody int64) *Session`
  - `func (s *Session) Snapshot() (file Values, runtime Values)`
  - `func (s *Session) Save(next Values) (Applied, error)`

- [ ] **Step 1: Write the failing test**

`Effective` with `Overrides{Proxy: true, MaxFlows: true}` returns the flag proxy and max flows, the file UI and max body, and the file theme.

`NewSession` + `Save` of a new proxy and a new `MaxFlows`:

- The file on disk has the new proxy and the new max flows.
- `Snapshot` runtime proxy is still the original listen address.
- `Applied.SetMaxFlows` is true only when `Overrides.MaxFlows` is false.
- When `Overrides.MaxFlows` is true, runtime `MaxFlows` stays at the flag value and `SetMaxFlows` is false.
- The same pair of assertions for `MaxBody` and `Overrides.MaxBody`.
- Theme in runtime always becomes the saved theme.
- `Save` of an illegal theme returns `ErrTheme` and leaves the file bytes unchanged.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/config/ -run 'TestEffective|TestSession' -count=1`

Expected: FAIL because `Effective` is undefined.

- [ ] **Step 3: Implement the session**

`Effective` copies `file`, then replaces proxy, UI, max flows, and max body for each true override field. It does not touch theme.

`NewSession` stores the dir, file, overrides, and `Effective(...)` as runtime. `Snapshot` returns copies under a mutex.

`Save` calls `dir.Save`. On error, return it and do not change memory. On success, set `file` to `next`. Leave `runtime.Proxy` and `runtime.UI` as they were. If `!ov.MaxFlows`, set runtime max flows from `next` and fill `Applied.SetMaxFlows` and `MaxFlows`. If `!ov.MaxBody`, do the same for the body limit. Always set `runtime.Theme = next.Theme`.

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/config/ -count=1`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/config/session.go internal/config/session_test.go
git commit -m "$(cat <<'EOF'
feat: keep cli overrides until restart

EOF
)"
```

---

### Task 4: Shrink the flow ring

**Files:**
- Modify: `internal/store/store.go`
- Modify: `internal/store/store_test.go`

**Interfaces:**
- Consumes: existing `Store.order` oldest-to-newest and `Store.byID`
- Produces: `func (s *Store) SetMax(max int) (evicted []string)`

- [ ] **Step 1: Write the failing test**

Add `TestSetMaxEvictsOldest` to `internal/store/store_test.go`. `store.New(3)`, add ids `1`, `2`, `3`. `SetMax(1)` returns `[]string{"1", "2"}` in that order. `Get("3")` is the only remaining flow. `SetMax(4)` returns an empty slice and `Max()` is 4. `SetMax(0)` clamps to 1, matching `New`.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/store/ -run TestSetMaxEvictsOldest -count=1`

Expected: FAIL because `SetMax` is undefined.

- [ ] **Step 3: Implement SetMax**

Lock `s.mu`. If `max < 1`, set `max = 1`. Assign `s.max`. While `len(s.order) > s.max`, delete `s.order[0]` from `byID`, append that id to `evicted`, and reslice `order`. Return `evicted`.

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/store/ -count=1`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/store/store.go internal/store/store_test.go
git commit -m "$(cat <<'EOF'
feat: resize the flow ring without a restart

EOF
)"
```

---

### Task 5: Freeze each flow's body limit

**Files:**
- Modify: `internal/proxy/proxy.go`
- Modify: `internal/proxy/http1.go`
- Modify: `internal/proxy/http2.go`
- Create: `internal/proxy/limit_test.go`

**Interfaces:**
- Consumes: `Proxy.maxBody`, `startFlow`, `setRequestCapture`, `finishResponse`, `failUpstream`, `handleHTTP2Request`
- Produces:
  - `func (p *Proxy) SetMaxBody(n int64)`
  - `func (p *Proxy) maxBodyLimit() int64`
  - `startFlow`, `setRequestCapture`, `failUpstream`, and `finishResponse` take the limit chosen at `startFlow`

- [ ] **Step 1: Write the failing test**

Create `internal/proxy/limit_test.go` in package `proxy`. Build `New("127.0.0.1:0", discardSink{}, 4, nil)`. POST `http://example.com/` with body `abcdefghij`. `startFlow` returns limit `4`. Read the request body to EOF. `reqCapture.result()` stores 4 bytes and reports truncated. `SetMaxBody(2)`. A second request's `startFlow` returns limit `2`. Finish the first flow through `finishResponse` with a response body of 8 bytes and a `bodyCapture` created with the first limit; the stored response prefix is 4 bytes, not 2.

Use a `FlowSink` whose methods are empty. `httptest.NewRequest` is enough. Do not listen.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/proxy/ -run TestFlowKeepsStartLimit -count=1`

Expected: FAIL to compile because `SetMaxBody` does not exist and `startFlow` returns one value.

- [ ] **Step 3: Thread the limit through the handlers**

`maxBodyLimit` and `SetMaxBody` take `p.mu`. Negative `SetMaxBody` stores 0.

`startFlow` reads `limit := p.maxBodyLimit()` once, passes it to `streamRequestBody`, and returns `(*bodyCapture, int64)`.

`setRequestCapture(f, reqCapture, req, limit)` uses `limit` in the `ContentLength` truncation check.

`failUpstream` and `finishResponse` take `limit int64`. `finishResponse` passes that limit to `decodeForDisplay`.

In `handleRequest` and `handleHTTP2Request`, keep the returned limit in a local. Pass it to `failUpstream` and `finishResponse`. Create the response `bodyCapture` with that local, not with `p.maxBody`. `tunnelWebSocket` calls `setRequestCapture` with the same local; thread `limit` into `tunnelWebSocket`.

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/proxy/ -count=1`

Expected: PASS, including the existing proxy tests.

- [ ] **Step 5: Commit**

```bash
git add internal/proxy/proxy.go internal/proxy/http1.go internal/proxy/http2.go internal/proxy/limit_test.go
git commit -m "$(cat <<'EOF'
feat: keep a flow on the body limit it started with

EOF
)"
```

---

### Task 6: Config HTTP API

**Files:**
- Create: `internal/api/config_http.go`
- Create: `internal/api/config_test.go`
- Modify: `internal/api/server.go`

**Interfaces:**
- Consumes: `config.Session`, `config.Values`, `config.ParseJSON`, `store.SetMax`, `proxy.SetMaxBody`, existing `writeJSON` and `Event`
- Produces:
  - `func New(st *store.Store, ca *cert.Manager, session *config.Session, px *proxy.Proxy) *Server`
  - `GET /api/config` and `PUT /api/config`
  - `func (s *Server) SetProxy(px *proxy.Proxy)`

- [ ] **Step 1: Write the failing test**

Create `internal/api/config_test.go`. `config.Open` a temp workdir. `NewSession` with no overrides and a proxy `New` on `127.0.0.1:0`. `api.New(store.New(sessionRuntimeMax), ca, session, px)`.

`GET /api/config` returns 200 and both `file.max_flows` and `runtime.max_flows` equal the default 1000.

`PUT` `{"proxy":"127.0.0.1:9090","ui":"127.0.0.1:9091","max_flows":1,"max_body":8,"theme":"light"}` returns 200. Response `file.proxy` is `127.0.0.1:9090`. Response `runtime.proxy` is still `127.0.0.1:8080`. `runtime.theme` is `light`. `store.Max()` is 1. `px.maxBodyLimit()` is 8.

Seed three flows with ids `1`, `2`, and `3` before that PUT. Dial `GET /ws/flows` the same way `server_test.go` does, read frames until two `flow.evicted` messages arrive, and require their `flow_id` values to be `1` then `2`. `store.Get("3")` still exists. `store.Get("1")` does not.

A second server with `Overrides{MaxFlows: true}` and effective max flows 50: PUT max flows 2 leaves `store.Max()` at 50 and `runtime.max_flows` at 50, while `file.max_flows` is 2.

PUT `{"max_flows":1}` returns 400 `{"error":"配置缺少字段"}` and the file bytes are unchanged.

PUT `{"proxy":"nope","ui":"127.0.0.1:8081","max_flows":1,"max_body":1,"theme":"dark"}` returns 400 `代理地址必须是 host:port`.

PUT `{` returns 400 `配置不是合法的 JSON`.

Update the two `api.New(st, ca)` calls in `internal/api/server_test.go` to `api.New(st, ca, nil, nil)`.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/api/ -count=1`

Expected: FAIL to compile because `New` still takes two arguments.

- [ ] **Step 3: Implement the handlers**

Change `New` to store `session` and `proxy`. `SetProxy` assigns `s.proxy`. `routes` keeps every current route and adds `PUT /api/config`.

`GET` with a nil session returns 500 `无法写入配置`. Otherwise `Snapshot` and `writeJSON` 200 of

```go
struct {
	File    config.Values `json:"file"`
	Runtime config.Values `json:"runtime"`
}{file, runtime}
```

`PUT` reads `io.ReadAll` with a 1 MiB limit. `config.ParseJSON`. Map errors with `errors.Is`:

| Sentinel | Status | error |
| --- | --- | --- |
| `ErrProxyAddress` | 400 | 代理地址必须是 host:port |
| `ErrUIAddress` | 400 | UI 地址必须是 host:port |
| `ErrMaxFlows` | 400 | 保留条数必须是大于等于 1 的整数 |
| `ErrMaxBody` | 400 | 正文上限必须是大于等于 0 的整数 |
| `ErrTheme` | 400 | 主题必须是 dark 或 light |
| `ErrMissingField` | 400 | 配置缺少字段 |
| `ErrUnknownField` | 400 | 配置含有未知字段 |
| `ErrJSON` | 400 | 配置不是合法的 JSON |
| any other error | 500 | 无法写入配置 |

On success, if `Applied.SetMaxFlows`, `ids := s.store.SetMax(Applied.MaxFlows)` and broadcast `Event{Type: "flow.evicted", FlowID: id}` for each id. If `Applied.SetMaxBody` and `s.proxy != nil`, `s.proxy.SetMaxBody`. Then write the same JSON shape as GET from `Applied.File` and `Applied.Runtime`.

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/api/ -count=1`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/api/config_http.go internal/api/config_test.go internal/api/server.go internal/api/server_test.go
git commit -m "$(cat <<'EOF'
feat: read and save proxy config over HTTP

EOF
)"
```

---

### Task 7: Command line and startup

**Files:**
- Create: `cmd/httpproxy/args.go`
- Create: `cmd/httpproxy/args_test.go`
- Modify: `cmd/httpproxy/main.go`

**Interfaces:**
- Consumes: `config.Open`, `config.ChmodPrivate`, `config.NewSession`, `config.Overrides`, `api.New`, `proxy.New`, `cert.LoadOrCreate`
- Produces:
  - `type cliOptions struct { workdir, proxy, ui string; maxFlows int; maxBody int64; proxySet, uiSet, maxFlowsSet, maxBodySet bool }`
  - `func parseArgs(args []string) (cliOptions, error)`
  - `func resolveWorkdir(dir string) (string, int, error)`
  - `func run(args []string) int`

- [ ] **Step 1: Write the failing test**

`parseArgs(nil)` has empty workdir and all four `*Set` fields false.

`parseArgs([]string{"-proxy", "127.0.0.1:1", "/tmp/proj"})` and `parseArgs([]string{"/tmp/proj", "-max-flows", "50"})` set the matching fields and the workdir.

`parseArgs([]string{"-proxy=127.0.0.1:2", "-ui", "127.0.0.1:3"})` sets both addresses.

`parseArgs([]string{"--", "-odd"})` treats `-odd` as the workdir, not a flag.

`parseArgs([]string{"a", "b"})` returns an error.

`parseArgs([]string{"-ca-dir", "/tmp"})` returns an error.

`resolveWorkdir("")` returns the current directory and code 0.

`resolveWorkdir` of a missing path returns code 2.

`resolveWorkdir` of a file returns code 2.

Do not listen in these tests. Add `TestRunRejectsBadFlagBeforeCreate` in `args_test.go`: under `t.TempDir()`, `chdir` is unnecessary if `run` receives an absolute missing directory. Call `run([]string{"-max-flows", "0", workdir})` where `workdir` exists. Exit code is 2. `<workdir>/.httpproxy` does not exist.

Call `run([]string{filepath.Join(t.TempDir(), "missing")})`. Exit code is 2.

Do not call `run` on a path that reaches a successful listen; that call blocks. Cover the success-path files with a listen failure instead: `run([]string{"-proxy", "127.0.0.1:1", "-ui", "127.0.0.1:1", workdir})` where `workdir` is `t.TempDir()`. Exit code is 1. `.httpproxy/config.yaml` exists, `ca.key` is mode `0600`, and the directory is `0700`. Task 7 does not change `ui.Handler`; it still takes no theme argument.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./cmd/httpproxy/ -count=1`

Expected: FAIL because `parseArgs` is undefined.

- [ ] **Step 3: Implement argv splitting and run**

Copy the httptest split: `flagTakesArg` is true for `proxy`, `ui`, `max-flows`, and `max-body` when the argument has no `=`. `splitCLIArgs` honors `--`. `parseArgs` builds a `flag.FlagSet` with `ContinueOnError` and output `io.Discard`. Defaults are empty string, 0, and 0. After `Parse`, `Visit` sets the four booleans. More than one positional returns `fmt.Errorf("unexpected extra arguments")`.

`resolveWorkdir` matches httptest: empty uses `os.Getwd` and code 1 on failure; a non-empty path uses `filepath.Abs`, `os.Stat`, and code 2 when missing or not a directory.

`run` prints flag and validation errors to stderr and returns 2. Before `Open`, copy `config.Defaults()` and replace only the fields whose `*Set` flag is true. `config.ParseYAML(config.MarshalYAML(candidate))` then rejects `-max-flows 0` and `-proxy nope`. Return 2 on that error. Do not call `Open` after it.

Then `resolveWorkdir`. Code 2 prints `workdir: %v`. Code 1 prints `getcwd: %v`.

`config.Open`. On error print `config: %v` and return 1.

`cert.LoadOrCreate(dir.Path())`. On error print `ca: %v` and return 1.

`config.ChmodPrivate(dir.Path())`. On error print `config: %v` and return 1.

`session := config.NewSession(dir, file, overrides, opt.proxy, opt.ui, opt.maxFlows, opt.maxBody)`.

`runtime :=` the second snapshot value. `store.New(runtime.MaxFlows)`. `api.New(st, ca, session, nil)`. `proxy.New(runtime.Proxy, apiServer.Sink(), runtime.MaxBody, ca)`. `apiServer.SetProxy(proxyServer)`.

Keep the current UI mux, `api.LocalOnly`, and the two `ListenAndServe` calls. Pass `runtime.UI` and `runtime.Proxy` instead of the flag pointers. Delete `-ca-dir` and the home-directory default. `log.Printf` the UI address, the proxy address, and `dir.Path()`.

`main` is `os.Exit(run(os.Args[1:]))`.

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./cmd/httpproxy/ ./internal/config/ ./internal/api/ -count=1`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/httpproxy/args.go cmd/httpproxy/args_test.go cmd/httpproxy/main.go
git commit -m "$(cat <<'EOF'
feat: start httpproxy from a work directory

EOF
)"
```

---

### Task 8: Theme on the embedded index

**Files:**
- Modify: `internal/ui/embed.go`
- Modify: `internal/ui/embed_test.go`
- Modify: `web/index.html`
- Modify: `cmd/httpproxy/main.go` call site `ui.Handler()`

**Interfaces:**
- Consumes: embedded `dist/index.html`
- Produces: `func Handler(theme string) http.Handler`

- [ ] **Step 1: Write the failing test**

Replace the `httpproxy.theme` assertion in `TestHandlerServesEmbeddedIndex`. `ui.Handler("light")` responses for `/` and `/flows/123` contain `lang="zh-CN"` and `data-theme="light"`. They do not contain `httpproxy.theme`. `ui.Handler("nope")` serves `data-theme="dark"`. The module script assertion stays.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/ui/ -count=1`

Expected: FAIL because `Handler` takes no argument or the body still has the old script and no `data-theme`.

- [ ] **Step 3: Inject the attribute**

`Handler` accepts `theme string`. Values other than `light` become `dark`. For a request whose path is `/` or has no `.` in the trimmed path, read `index.html` from the embedded sub filesystem. Replace the first `<html lang="zh-CN">` with `<html lang="zh-CN" data-theme="` + theme + `">`. Cut out the inline script that contains `httpproxy.theme`, from its `<script>` through the matching `</script>`, if that script is still in the built file. Write `Content-Type: text/html; charset=utf-8` and the body. Every other path still uses the file server.

Delete the theme `<script>` block from `web/index.html`. Leave `<html lang="zh-CN">` without `data-theme` so Vite stays on the dark `:root` until the app fetches config.

In `cmd/httpproxy/main.go`, call `ui.Handler(runtime.Theme)`.

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/ui/ ./cmd/httpproxy/ -count=1`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/ui/embed.go internal/ui/embed_test.go web/index.html cmd/httpproxy/main.go
git commit -m "$(cat <<'EOF'
feat: serve the index with the configured theme

EOF
)"
```

---

### Task 9: Settings shell

**Files:**
- Create: `web/src/settings.ts`
- Create: `web/settings.test.ts`
- Modify: `web/src/types.ts`
- Modify: `web/src/api.ts`
- Modify: `web/src/App.tsx`
- Modify: `web/src/components/TopBar.tsx`
- Create: `web/src/components/ActivityBar.tsx`
- Create: `web/src/components/SettingsPane.tsx`
- Modify: `web/src/styles.css`
- Modify: `web/package.json` test script if `settings.test.ts` is not already included by the current `node --test` command

**Interfaces:**
- Consumes: `GET /api/config` and `PUT /api/config`
- Produces:
  - `export interface ConfigValues { proxy: string; ui: string; max_flows: number; max_body: number; theme: 'dark' | 'light' }`
  - `export interface ConfigSnapshot { file: ConfigValues; runtime: ConfigValues }`
  - `export function limitNote(file: number, runtime: number): string | null`
  - `getConfig(): Promise<ConfigSnapshot>`
  - `putConfig(file: ConfigValues): Promise<ConfigSnapshot>`
  - `ActivityBar` and `SettingsPane`

- [ ] **Step 1: Write the failing test**

Create `web/settings.test.ts` using `node:test` and `node:assert/strict`, same style as `web/columns.test.ts`. `limitNote(1000, 1000)` is `null`. `limitNote(1000, 50)` is `本次实际 50`.

Change the `test` script in `web/package.json` to:

```json
"test": "node --experimental-strip-types --test columns.test.ts settings.test.ts"
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd web && npm test`

Expected: FAIL because `web/src/settings.ts` does not exist.

- [ ] **Step 3: Implement the shell**

`web/src/settings.ts` exports `limitNote`. It returns `null` when the numbers are equal, otherwise `` `本次实际 ${runtime}` ``.

Replace `AppConfig` in `web/src/types.ts` with `ConfigValues` and `ConfigSnapshot`.

`getConfig` returns `ConfigSnapshot`. `putConfig` sends `PUT /api/config` with `Content-Type: application/json`. On `!r.ok`, throw an `Error` whose message is the JSON `error` string when present, otherwise `配置不是合法的 JSON` is not invented here: use `无法写入配置` when the body has no `error`.

`TopBar` props are `onClear`, `count`, and `proxy: string`. Delete the theme state, `readTheme`, `applyTheme`, and the theme button. Render `代理` and `<code>{proxy}</code>` with the existing title `将客户端的 HTTP(S) 代理指向此地址`. Keep the count, `下载 CA`, and `清空`.

`ActivityBar` props are `open: boolean` and `onToggle: () => void`. Render `<nav className="activity" aria-label="侧栏">`, a spacer, and a button `aria-label="设置"` `title="设置"` `aria-pressed={open}` class `activity-btn` plus `active` when `open`. The icon is an inline 16×16 gear SVG, `stroke="currentColor"`, no extra dependency.

`SettingsPane` props are `snapshot: ConfigSnapshot | null`, `loadError: boolean`, and `onSaved: (snapshot: ConfigSnapshot) => void`.

When `loadError || snapshot == null`, render `<p className="settings-error">无法读取配置</p>` and no inputs.

Otherwise copy `snapshot.file` into draft state when the file identity changes (`proxy`, `ui`, both numbers, and `theme` as the effect dependency). Fields:

- 主题: two buttons, `黑色` and `浅色`, setting draft theme to `dark` or `light`. The selected one has class `seg-btn active`.
- 代理地址 and UI 地址: text inputs bound to the draft. Beside each, `<span className="settings-note">当前 {snapshot.runtime.proxy or ui}</span>` and `<span className="settings-note">重启后生效</span>`.
- 保留条数 and 正文上限: text inputs whose draft state is a string so the user can clear the box. Initialize from `String(file.max_flows)` and `String(file.max_body)`. Beside each, render `limitNote` when it is non-null.
- Button `保存`. Disabled while `saving` is true.

On submit, `Number` the two count strings. If either is not an integer (`!Number.isInteger`), still PUT the JSON and let the server answer; send `max_flows` and `max_body` as numbers when they are integers, and omit nothing. The server requires the five fields. If the draft is not an integer, PUT is still attempted with `Math.trunc` removed: send the raw `Number` result, which will be rejected when it is `NaN` because `JSON.stringify` turns `NaN` into `null`. That returns `配置缺少字段`. That matches the API. Do not add a second client-side ruleset.

On success, call `onSaved`. On failure, keep the draft and show `err.message` in `.settings-error` beside the button.

`App` holds `snapshot`, `loadError`, and `view: 'capture' | 'settings'` starting at `capture`. The initial `getConfig` effect sets `snapshot` and, on success, `document.documentElement.dataset.theme = snapshot.runtime.theme`. On failure set `loadError` and leave the theme attribute alone. `maxFlows` state becomes `snapshot?.runtime.max_flows ?? 1000`.

`onSaved` sets the snapshot, updates `maxFlows` from `runtime.max_flows`, and sets `data-theme` from `runtime.theme`.

Render `.app` > `TopBar` > `.shell`. `TopBar` `proxy` is `snapshot?.runtime.proxy ?? ''` until the GET returns; do not hardcode port 8080. Inside `.shell`, `ActivityBar` then either `SettingsPane` or the existing filter bar plus `.workspace`. Do not unmount the WebSocket effect when the view changes. `onClear` stays in `App`.

CSS, appended to `web/src/styles.css`:

```css
:root { --activity: #010409; }
[data-theme="light"] { --activity: #f6f8fa; }
.shell { flex: 1; display: flex; min-height: 0; min-width: 0; }
.activity {
  flex: 0 0 32px; width: 32px;
  display: flex; flex-direction: column;
  background: var(--activity);
  border-right: 1px solid var(--border);
}
.activity-spacer { flex: 1; }
.activity-btn {
  height: 32px; width: 32px; padding: 0;
  display: flex; align-items: center; justify-content: center;
  border: 0; background: transparent; color: var(--text-muted); cursor: pointer;
  position: relative;
}
.activity-btn:hover { color: var(--text); }
.activity-btn.active { color: var(--text); }
.activity-btn.active::before {
  content: ""; position: absolute; left: 0; top: 6px; bottom: 6px;
  width: 2px; background: var(--accent);
}
.settings { flex: 1; min-width: 0; overflow: auto; padding: 16px 20px; }
.settings h1 { margin: 0 0 12px; font-size: 13px; font-weight: 600; }
.settings-row {
  display: flex; align-items: center; gap: 8px;
  min-height: 28px; margin-bottom: 4px;
}
.settings-label { width: 72px; flex: none; color: var(--text-muted); }
.settings-note { color: var(--warn); }
.settings-error { color: var(--danger); margin: 8px 0 0; }
.seg-btn.active { color: var(--text); border-color: var(--accent); }
```

The capture column (filter bar and workspace) needs `flex: 1; min-width: 0; display: flex; flex-direction: column`. Add class `capture` on that wrapper.

- [ ] **Step 4: Run the tests and the build**

Run: `cd web && npm test && npm run build`

Expected: PASS. `tsc` accepts the new components.

- [ ] **Step 5: Commit**

```bash
git add web/src/settings.ts web/settings.test.ts web/src/types.ts web/src/api.ts web/src/App.tsx web/src/components/TopBar.tsx web/src/components/ActivityBar.tsx web/src/components/SettingsPane.tsx web/src/styles.css web/package.json web/index.html
git commit -m "$(cat <<'EOF'
feat: add the settings activity and form

EOF
)"
```

`web/index.html` is listed again because Task 8 may have committed it already. If it has no new diff, leave it out of this commit.

---

### Task 10: README and embedded build

**Files:**
- Modify: `README.md`
- Modify: `internal/ui/dist/**` by rebuilding, only the files `npm run build` changes

**Interfaces:**
- Consumes: the behavior from Tasks 1–9
- Produces: an English README and a dist whose served index has no `httpproxy.theme` script

- [ ] **Step 1: Update the README**

In `README.md`:

- Replace the startup sentence that says the CA is generated under `~/.httpproxy` with: the CA and `config.yaml` are created under `<workdir>/.httpproxy/` on first launch. The workdir is the optional directory argument, or the current directory when it is omitted. `httpproxy` does not read `~/.httpproxy`.
- Change the run example to:

```sh
./bin/httpproxy
./bin/httpproxy /path/to/workdir
```

- Replace the flag table. Remove `-ca-dir`. Document the positional directory and the four flags as this-process overrides. State that `-proxy` and `-ui` apply only after a restart when changed from the settings page, and that settings edits are written to `.httpproxy/config.yaml`.
- Add the tree from the spec: `.httpproxy/.gitignore`, `config.yaml`, `ca.crt`, `ca.key`. Say `.gitignore` contains `*` and `ca.key` is mode `0600`.
- Say the gear at the bottom of the left bar opens settings for theme, proxy address, UI address, retained flow count, and body limit. The top bar still has CA download and clear.
- Delete the LAN example's implication that `-ca-dir` exists. Keep a loopback-first warning if the README already has one, using `-proxy` and `-ui` only.

- [ ] **Step 2: Rebuild the embedded UI**

Run: `cd web && npm run build`

`web/vite.config.ts` sets `build.outDir` to `../internal/ui/dist` and `emptyOutDir` to true, so this command replaces that directory. Do not copy a second dist folder.

Run: `go test ./...`

Expected: PASS. `internal/ui` tests see `data-theme` and do not require `httpproxy.theme`.

Run: `cd web && npm test`

Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add README.md internal/ui/dist
git commit -m "$(cat <<'EOF'
docs: describe the workdir and settings page

EOF
)"
```

If `internal/ui/dist` contains unrelated uncommitted hunks from before this work, do not stage those hunks. Stage only the build output this task produced. If that cannot be separated, stop and report the conflict instead of mixing the commits.

---

## Spec coverage

| Spec requirement | Task |
| --- | --- |
| `httpproxy [DIR]`, flag position, `--`, exit 2 before create | Task 7 |
| `.httpproxy` mode, gitignore, default YAML, illegal file kept, CA key mode | Task 2, Task 7 |
| Strict YAML and JSON, address forms, Chinese API errors | Task 1, Task 6 |
| Overrides stick; addresses do not rebind; limits and theme follow the save rules | Task 3, Task 6 |
| Ring shrink emits `flow.evicted` | Task 4, Task 6 |
| Body limit frozen per flow | Task 5 |
| 32px activity bar, settings form, top bar proxy and buttons | Task 9 |
| Index `data-theme`, no `httpproxy.theme` script | Task 8, Task 9, Task 10 |
| README | Task 10 |
| No migration, no in-app restart, no column move, no new npm package | Global Constraints, no task adds them |
