# Capture Switch And Host Allowlist Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let the user pause capture or record only allowlisted hosts, while non-recorded HTTPS stays decrypted or becomes a raw tunnel.

**Architecture:** `internal/capture` compiles four config fields into `Record`, `DecryptOnly`, or `Tunnel`. `config.Values` stores the raw fields and calls `Compile` while parsing. The proxy snapshots that policy at each new CONNECT and each plain HTTP request. Settings saves the whole file. The top bar writes only `capture` through `PUT /api/capture`.

**Tech Stack:** Go 1.24.4, existing `gopkg.in/yaml.v3`, `net/http`, React 19, TypeScript, Vite, `node --test`. No new dependency.

**Spec:** `docs/superpowers/specs/2026-10-28-capture-policy-design.md`

## Global Constraints

- Defaults are `capture: true`, `capture_empty: all`, `capture_https: decrypt`, `capture_hosts: []`. These four fields have no CLI override. After a successful parse, `file` and `runtime` hold the same four values.
- YAML read: a missing key or YAML null uses the default and does not rewrite the file. The original five keys stay required. Unknown keys stay illegal. `PUT /api/config` JSON must include all four new keys.
- `capture` is a boolean. The string `"true"` is illegal. `capture_empty` is `all` or `none`. `capture_https` is `decrypt` or `tunnel`. `capture_hosts` is a string list, order preserved, at most 100 items, each at most 253 characters after trim.
- A rule is only `host`, `host:port`, `*.host`, or `*.host:port`. IPv6 is `[::1]` or `[::1]:443`. `*` is only the leading `*.`, and the rest is a DNS name, not an IP. Ports are 1 through 65535. Scheme, path, userinfo, spaces, empty labels, a leading or trailing dot, and non-ASCII are illegal.
- Duplicates compare trimmed text, case-insensitive host, wildcard flag, and the same port set. `example.com` and `example.com:443` may both be stored. Stored text keeps submitted case and order.
- Omitted port matches 80 and 443 only. `*.example.com` matches `a.example.com` and `a.b.example.com`, and does not match `example.com` or `notexample.com`.
- CONNECT decides once from the request-line host, else `Host`. Later requests on that tunnel, including HTTP/2, keep that action and ignore the inner Host and SNI. Plain HTTP decides per request. A missing port means 80 for HTTP and 443 for HTTPS. Plain HTTP never returns `Tunnel`.
- Capture off records nothing and uses `capture_https` for HTTPS. Capture on with an empty list uses `capture_empty`. A non-empty list records a hit on any rule.
- Tunnel dials with `net.Dialer{Timeout: 30s, KeepAlive: 30s}` before `200`. Self-proxy returns 502 with body `refusing to proxy to the proxy's own listen address\n`. A missing dial port or a dial error returns 502 with body `Bad Gateway\n` and does not send `200`. Splice uses the buffered connection, clears deadlines, and closes both sides when either copy ends. Tunnel does not use the CA or the warm connection pool.
- Record and `DecryptOnly` still require the CA. The missing-CA body stays `HTTPS interception requires a CA\n`. `DecryptOnly` does not call `newBodyCapture` or the sink. TLS handshake failure is stored only for `Record`.
- `Proxy.New` records everything until `SetPolicy`. `SetPolicy` affects only later decisions. An in-flight CONNECT keeps its action. The next plain request on a keep-alive connection uses the new policy. Existing flows stay until clear.
- English errors: `capture must be a boolean`, `capture_empty must be all or none`, `capture_https must be decrypt or tunnel`, `capture_hosts must be a list of strings`, `capture_hosts must contain at most 100 rules`, `a capture host rule must be at most 253 characters`, `invalid capture host rule`, `duplicate capture host rule`. Check order is type, count, length, syntax, duplicate.
- HTTP 400 strings: `抓包开关必须是布尔值`, `空规则必须是 all 或 none`, `未抓 HTTPS 必须是 decrypt 或 tunnel`, `主机规则必须是字符串列表`, `主机规则最多 100 条`, `主机规则不能超过 253 个字符`, `主机规则不合法`, `主机规则重复`. Keep `配置缺少字段`, `配置含有未知字段`, `无法写入配置`. `PUT /api/capture` accepts only `{"capture": true}` or `{"capture": false}`.
- UI copy is exactly: button `暂停抓包` / `开始抓包`; summary segments joined by ` · `; `抓包已暂停`; `空规则：抓全部` / `空规则：不抓`; `未抓 HTTPS：解密` / `未抓 HTTPS：隧道`; `主机：无`; host separator `、`; more than 3 hosts show the first 3 plus ` 等 N 条` where N is the total; `title` is one host per line, or `无`; unloaded summary is `正在读取配置`; settings note is `保存后立即生效`.
- Do not add a WebSocket event, a CLI flag, a browser test, or a path/method/status capture rule. Do not clear flows when the policy changes. `git add` only the paths named in the task. Never `git add -A`. The worktree has unrelated edits in `internal/proxy/http2.go` and `internal/proxy/warm_test.go`; leave them untouched unless a task names that file and the edit is the one shown here.

## File Structure

| Path | Responsibility |
| --- | --- |
| `internal/capture/policy.go` | Compile rules and decide one action |
| `internal/capture/policy_test.go` | Match and action tests |
| `internal/config/value.go` | Four fields, YAML defaults, JSON required keys, marshal |
| `internal/config/session.go` | Copy the four fields into runtime on save |
| `internal/api/config_http.go` | Chinese errors, `SetPolicy`, `PUT /api/capture` |
| `internal/api/server.go` | Route `PUT /api/capture` |
| `internal/proxy/proxy.go` | Policy snapshot and `SetPolicy` |
| `internal/proxy/http1.go` | CONNECT branch, splice, skip capture |
| `internal/proxy/http2.go` | Skip capture on an already decided tunnel |
| `internal/proxy/body.go` | Count `newBodyCapture` calls for the skip test |
| `cmd/httpproxy/main.go` | `SetPolicy` before accept |
| `web/src/capture.ts` | Summary text and settings payload |
| `web/src/components/TopBar.tsx`, `SettingsPane.tsx`, `App.tsx`, `web/src/styles.css` | Toggle, summary, form |
| `README.md` | Default key list |

---

### Task 1: Capture policy

**Files:**
- Create: `internal/capture/policy.go`
- Create: `internal/capture/policy_test.go`
- Test: `internal/capture/policy_test.go`

**Interfaces:**
- Consumes: nothing
- Produces:
  - `type Action int` with `Record Action = iota`, `DecryptOnly`, `Tunnel`
  - `func Default() Policy`
  - `func Compile(capture bool, empty, httpsMode string, hosts []string) (Policy, error)`
  - `func (p Policy) Decide(https bool, authority string) Action`
  - Sentinels whose `Error()` strings are the eight English sentences in Global Constraints

- [ ] **Step 1: Write the failing test**

Create `internal/capture/policy_test.go`:

```go
package capture

import (
	"errors"
	"strings"
	"testing"
)

func TestDecideRecordsByDefault(t *testing.T) {
	if got := Default().Decide(true, "example.com:443"); got != Record {
		t.Fatalf("action %d", got)
	}
	if got := Default().Decide(false, "example.com"); got != Record {
		t.Fatalf("plain %d", got)
	}
}

func TestDecideHostRules(t *testing.T) {
	p, err := Compile(true, "all", "decrypt", []string{
		" Example.COM ",
		"*.Example.com",
		"example.com:8443",
		"[::1]",
	})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		https bool
		host  string
		want  Action
	}{
		{true, "example.com:443", Record},
		{true, "example.com:80", Record},
		{true, "example.com:8443", Record},
		{true, "example.com:8444", DecryptOnly},
		{true, "api.example.com:443", Record},
		{true, "a.b.example.com:443", Record},
		{true, "notexample.com:443", DecryptOnly},
		{false, "example.com", Record},
		{false, "example.com:443", Record},
		{false, "example.com:8444", DecryptOnly},
		{true, "[::1]:443", Record},
		{true, "[::1]:8443", DecryptOnly},
		{true, "[0:0:0:0:0:0:0:1]:80", Record},
	}
	for _, tc := range cases {
		if got := p.Decide(tc.https, tc.host); got != tc.want {
			t.Fatalf("Decide(%v, %s) = %d, want %d", tc.https, tc.host, got, tc.want)
		}
	}
}

func TestDecideEmptyListAndSwitch(t *testing.T) {
	all, err := Compile(true, "all", "tunnel", nil)
	if err != nil {
		t.Fatal(err)
	}
	if all.Decide(true, "a.test:443") != Record {
		t.Fatal("empty all should record")
	}
	none, err := Compile(true, "none", "tunnel", []string{})
	if err != nil {
		t.Fatal(err)
	}
	if none.Decide(true, "a.test:443") != Tunnel {
		t.Fatal("empty none https")
	}
	if none.Decide(false, "a.test") != DecryptOnly {
		t.Fatal("plain http must not tunnel")
	}
	off, err := Compile(false, "all", "decrypt", []string{"example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if off.Decide(true, "example.com:443") != DecryptOnly {
		t.Fatal("switch off ignores the allowlist when https mode is decrypt")
	}
	offTunnel, err := Compile(false, "all", "tunnel", []string{"example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if offTunnel.Decide(true, "example.com:443") != Tunnel {
		t.Fatal("switch off uses capture_https")
	}
}

func TestCompileRejectsRules(t *testing.T) {
	long := strings.Repeat("a", 254)
	many := make([]string, 101)
	for i := range many {
		many[i] = "h" + strings.Itoa(i) + ".example.com"
	}
	cases := []struct {
		empty string
		https string
		hosts []string
		want  error
	}{
		{"all", "nope", nil, ErrCaptureHTTPS},
		{"nope", "decrypt", nil, ErrCaptureEmpty},
		{"all", "decrypt", []string{"https://example.com"}, ErrCaptureHost},
		{"all", "decrypt", []string{"example.com/a"}, ErrCaptureHost},
		{"all", "decrypt", []string{"*"}, ErrCaptureHost},
		{"all", "decrypt", []string{"*.[::1]"}, ErrCaptureHost},
		{"all", "decrypt", []string{"example.com:0"}, ErrCaptureHost},
		{"all", "decrypt", []string{"example.com:65536"}, ErrCaptureHost},
		{"all", "decrypt", []string{".example.com"}, ErrCaptureHost},
		{"all", "decrypt", []string{"ex ample.com"}, ErrCaptureHost},
		{"all", "decrypt", []string{"例.com"}, ErrCaptureHost},
		{"all", "decrypt", []string{long}, ErrCaptureHostLength},
		{"all", "decrypt", many, ErrCaptureHostsLimit},
		{"all", "decrypt", []string{"Example.com", "example.com"}, ErrCaptureHostDup},
		{"all", "decrypt", []string{"*.A.com", "*.a.com"}, ErrCaptureHostDup},
	}
	for _, tc := range cases {
		_, err := Compile(true, tc.empty, tc.https, tc.hosts)
		if !errors.Is(err, tc.want) {
			t.Fatalf("Compile(%q, %q, %v) err %v, want %v", tc.empty, tc.https, tc.hosts, err, tc.want)
		}
	}
	if _, err := Compile(true, "all", "decrypt", []string{"example.com", "example.com:443"}); err != nil {
		t.Fatal(err)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/capture/ -count=1`

Expected: FAIL because package `capture` does not exist.

- [ ] **Step 3: Write the implementation**

Create `internal/capture/policy.go`:

```go
package capture

import (
	"errors"
	"net"
	"strconv"
	"strings"
	"unicode"
)

type Action int

const (
	Record Action = iota
	DecryptOnly
	Tunnel
)

var (
	ErrCapture           = errors.New("capture must be a boolean")
	ErrCaptureEmpty      = errors.New("capture_empty must be all or none")
	ErrCaptureHTTPS      = errors.New("capture_https must be decrypt or tunnel")
	ErrCaptureHosts      = errors.New("capture_hosts must be a list of strings")
	ErrCaptureHostsLimit = errors.New("capture_hosts must contain at most 100 rules")
	ErrCaptureHostLength = errors.New("a capture host rule must be at most 253 characters")
	ErrCaptureHost       = errors.New("invalid capture host rule")
	ErrCaptureHostDup    = errors.New("duplicate capture host rule")
)

type rule struct {
	wildcard bool
	host     string
	ip       net.IP
	ports    [2]int // [0] set; [1] is 0 when only ports[0] counts. 0,0 means 80 and 443.
	anyPort  bool
}

type Policy struct {
	capture  bool
	emptyAll bool
	tunnel   bool
	rules    []rule
}

func Default() Policy {
	return Policy{capture: true, emptyAll: true}
}

func Compile(capture bool, empty, httpsMode string, hosts []string) (Policy, error) {
	if empty != "all" && empty != "none" {
		return Policy{}, ErrCaptureEmpty
	}
	if httpsMode != "decrypt" && httpsMode != "tunnel" {
		return Policy{}, ErrCaptureHTTPS
	}
	if len(hosts) > 100 {
		return Policy{}, ErrCaptureHostsLimit
	}
	rules := make([]rule, 0, len(hosts))
	seen := make(map[string]struct{}, len(hosts))
	for _, raw := range hosts {
		trimmed := strings.TrimSpace(raw)
		if len(trimmed) > 253 {
			return Policy{}, ErrCaptureHostLength
		}
		parsed, key, err := parseRule(trimmed)
		if err != nil {
			return Policy{}, err
		}
		if _, ok := seen[key]; ok {
			return Policy{}, ErrCaptureHostDup
		}
		seen[key] = struct{}{}
		rules = append(rules, parsed)
	}
	return Policy{
		capture:  capture,
		emptyAll: empty == "all",
		tunnel:   httpsMode == "tunnel",
		rules:    rules,
	}, nil
}

func (p Policy) Decide(https bool, authority string) Action {
	if p.records(https, authority) {
		return Record
	}
	if https && p.tunnel {
		return Tunnel
	}
	return DecryptOnly
}

func (p Policy) records(https bool, authority string) bool {
	if !p.capture {
		return false
	}
	if len(p.rules) == 0 {
		return p.emptyAll
	}
	host, port := splitAuthority(https, authority)
	for _, item := range p.rules {
		if item.matches(host, port) {
			return true
		}
	}
	return false
}

func splitAuthority(https bool, authority string) (string, int) {
	port := 80
	if https {
		port = 443
	}
	if host, text, err := net.SplitHostPort(authority); err == nil {
		if n, err := strconv.Atoi(text); err == nil {
			return host, n
		}
		return host, port
	}
	host := authority
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		host = host[1 : len(host)-1]
	}
	return host, port
}

func parseRule(s string) (rule, string, error) {
	for _, r := range s {
		if r > unicode.MaxASCII || unicode.IsSpace(r) {
			return rule{}, "", ErrCaptureHost
		}
	}
	if strings.Contains(s, "://") || strings.Contains(s, "/") || strings.Contains(s, "@") || (strings.Contains(s, "*") && !strings.HasPrefix(s, "*.")) {
		return rule{}, "", ErrCaptureHost
	}
	wildcard := false
	rest := s
	if strings.HasPrefix(s, "*.") {
		wildcard = true
		rest = s[2:]
		if strings.Contains(rest, "*") {
			return rule{}, "", ErrCaptureHost
		}
	}
	host, portText, err := splitPattern(rest)
	if err != nil {
		return rule{}, "", err
	}
	parsed := rule{wildcard: wildcard, anyPort: portText == ""}
	if portText == "" {
		parsed.ports = [2]int{80, 443}
	} else {
		n, err := strconv.Atoi(portText)
		if err != nil || n < 1 || n > 65535 || portText != strconv.Itoa(n) {
			return rule{}, "", ErrCaptureHost
		}
		parsed.ports = [2]int{n, 0}
	}
	if ip := net.ParseIP(host); ip != nil {
		if wildcard {
			return rule{}, "", ErrCaptureHost
		}
		parsed.ip = ip
		parsed.host = ip.String()
	} else {
		if !validDNS(host) {
			return rule{}, "", ErrCaptureHost
		}
		parsed.host = strings.ToLower(host)
	}
	key := "exact"
	if wildcard {
		key = "wild"
	}
	key += " " + parsed.host + " " + strconv.Itoa(parsed.ports[0]) + " " + strconv.Itoa(parsed.ports[1])
	return parsed, key, nil
}

func splitPattern(s string) (string, string, error) {
	if strings.HasPrefix(s, "[") {
		end := strings.IndexByte(s, ']')
		if end <= 1 {
			return "", "", ErrCaptureHost
		}
		host := s[1:end]
		tail := s[end+1:]
		if tail == "" {
			return host, "", nil
		}
		if len(tail) < 2 || tail[0] != ':' {
			return "", "", ErrCaptureHost
		}
		return host, tail[1:], nil
	}
	if strings.Count(s, ":") > 1 {
		return "", "", ErrCaptureHost
	}
	host, port, ok := strings.Cut(s, ":")
	if !ok {
		return s, "", nil
	}
	return host, port, nil
}

func validDNS(host string) bool {
	if host == "" || strings.HasPrefix(host, ".") || strings.HasSuffix(host, ".") {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			switch {
			case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-':
			default:
				return false
			}
		}
	}
	return true
}

func (item rule) matches(host string, port int) bool {
	if item.anyPort {
		if port != 80 && port != 443 {
			return false
		}
	} else if port != item.ports[0] {
		return false
	}
	if item.ip != nil {
		return item.ip.Equal(net.ParseIP(host))
	}
	host = strings.ToLower(host)
	if item.wildcard {
		return host != item.host && strings.HasSuffix(host, "."+item.host)
	}
	return host == item.host
}
```

`anyPort` is true when the rule omits a port. `matches` then allows only 80 and 443. A rule with an explicit port compares `ports[0]` only.

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/capture/ -count=1`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/capture/policy.go internal/capture/policy_test.go
git commit -m "feat: match capture hosts to one proxy action"
```

---

### Task 2: Config fields

**Files:**
- Modify: `internal/config/value.go`
- Modify: `internal/config/value_test.go`
- Modify: `internal/config/session.go`
- Modify: `internal/config/session_test.go`
- Modify: `internal/config/dir_test.go`
- Modify: `internal/api/config_test.go`
- Test: `internal/config/value_test.go`

**Interfaces:**
- Consumes: `capture.Compile`, `capture.ErrCapture`, `capture.ErrCaptureEmpty`, `capture.ErrCaptureHTTPS`, `capture.ErrCaptureHosts`, `capture.ErrCaptureHostsLimit`, `capture.ErrCaptureHostLength`, `capture.ErrCaptureHost`, `capture.ErrCaptureHostDup`
- Produces: `Values` fields `Capture bool`, `CaptureEmpty string`, `CaptureHTTPS string`, `CaptureHosts []string` with json and yaml tags `capture`, `capture_empty`, `capture_https`, `capture_hosts`. `Defaults`, `ParseYAML`, `ParseJSON`, and `MarshalYAML` understand them. `CaptureHosts` is an empty non-nil slice when absent. `SaveAndApply` copies all four onto `runtime` with no override flag.

- [ ] **Step 1: Write the failing test**

Add to `internal/config/value_test.go`:

```go
func TestParseYAMLDefaultsCaptureFields(t *testing.T) {
	raw := []byte("proxy: \"127.0.0.1:8080\"\nui: \"127.0.0.1:8081\"\nmax_flows: 1000\nmax_body: 1048576\ntheme: dark\n")
	got, err := ParseYAML(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Capture || got.CaptureEmpty != "all" || got.CaptureHTTPS != "decrypt" || len(got.CaptureHosts) != 0 || got.CaptureHosts == nil {
		t.Fatalf("%+v", got)
	}
}

func TestParseYAMLRejectsBadCapture(t *testing.T) {
	base := "proxy: \"127.0.0.1:8080\"\nui: \"127.0.0.1:8081\"\nmax_flows: 1\nmax_body: 1\ntheme: dark\n"
	cases := []struct {
		extra string
		want  error
	}{
		{"capture: \"true\"\ncapture_empty: all\ncapture_https: decrypt\ncapture_hosts: []\n", ErrCapture},
		{"capture: true\ncapture_empty: maybe\ncapture_https: decrypt\ncapture_hosts: []\n", ErrCaptureEmpty},
		{"capture: true\ncapture_empty: all\ncapture_https: pass\ncapture_hosts: []\n", ErrCaptureHTTPS},
		{"capture: true\ncapture_empty: all\ncapture_https: decrypt\ncapture_hosts: example.com\n", ErrCaptureHosts},
		{"capture: true\ncapture_empty: all\ncapture_https: decrypt\ncapture_hosts: [\"*.example.com\", \"*.Example.com\"]\n", ErrCaptureHostDup},
	}
	for _, tc := range cases {
		_, err := ParseYAML([]byte(base + tc.extra))
		if !errors.Is(err, tc.want) {
			t.Fatalf("extra %q err %v", tc.extra, err)
		}
	}
}

func TestParseJSONRequiresCaptureFields(t *testing.T) {
	_, err := ParseJSON([]byte(`{"proxy":"127.0.0.1:8080","ui":"127.0.0.1:8081","max_flows":1,"max_body":1,"theme":"dark"}`))
	if !errors.Is(err, ErrMissingField) {
		t.Fatal(err)
	}
	raw := []byte(`{"proxy":"127.0.0.1:8080","ui":"127.0.0.1:8081","max_flows":1,"max_body":1,"theme":"dark","capture":true,"capture_empty":"all","capture_https":"decrypt","capture_hosts":["Example.com"]}`)
	got, err := ParseJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Capture || got.CaptureHosts[0] != "Example.com" {
		t.Fatalf("%+v", got)
	}
	text := string(MarshalYAML(got))
	if !strings.Contains(text, "capture: true\n") || !strings.Contains(text, "capture_hosts:\n") || !strings.Contains(text, "\"Example.com\"") {
		t.Fatalf("%s", text)
	}
}
```

Import `httpproxy/internal/capture` is not required in the test if the sentinels are re-exported from `config` as the same values. Do that: `var ErrCapture = capture.ErrCapture` and the same for the other seven names, in `value.go`.

In `TestDefaultsAndRoundTrip`, extend both key slices to:

```go
[]string{"proxy:", "ui:", "max_flows:", "max_body:", "theme:", "capture:", "capture_empty:", "capture_https:", "capture_hosts:"}
```

Replace `if got != Defaults()` with `if !reflect.DeepEqual(got, Defaults())`. Add `"reflect"` to the imports.

Replace these comparisons that will stop compiling once `Values` contains a slice:

- `internal/config/dir_test.go`: `got != Defaults()` → `!reflect.DeepEqual(got, Defaults())`
- `internal/config/session_test.go`: every `got != want`, `gotFile != next`, and `applied.File != gotFile || applied.Runtime != gotRuntime` → `reflect.DeepEqual`
- `internal/config/value_test.go`: the existing `got != Defaults()`

In `internal/api/config_test.go` and `internal/config/value_test.go`, every complete config JSON object gains this suffix immediately before the closing `}`:

```text
,"capture":true,"capture_empty":"all","capture_https":"decrypt","capture_hosts":[]
```

Leave incomplete documents unchanged: `{"max_flows":1}` and the document that only omits `proxy`. A document that already ends with `"theme":"dark"` becomes `"theme":"dark","capture":true,"capture_empty":"all","capture_https":"decrypt","capture_hosts":[]}`.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/config/ ./internal/api/ -count=1`

Expected: FAIL compiling or failing the new capture assertions.

- [ ] **Step 3: Parse and save the four fields**

In `internal/config/value.go`:

```go
type Values struct {
	Proxy         string   `json:"proxy" yaml:"proxy"`
	UI            string   `json:"ui" yaml:"ui"`
	MaxFlows      int      `json:"max_flows" yaml:"max_flows"`
	MaxBody       int64    `json:"max_body" yaml:"max_body"`
	Theme         string   `json:"theme" yaml:"theme"`
	Capture       bool     `json:"capture" yaml:"capture"`
	CaptureEmpty  string   `json:"capture_empty" yaml:"capture_empty"`
	CaptureHTTPS  string   `json:"capture_https" yaml:"capture_https"`
	CaptureHosts  []string `json:"capture_hosts" yaml:"capture_hosts"`
}
```

Add the eight `var Err... = capture.Err...` aliases. Add the four keys to `knownFields`. `Defaults` sets `Capture: true`, `CaptureEmpty: "all"`, `CaptureHTTPS: "decrypt"`, `CaptureHosts: []string{}`.

At the start of both parse functions, after `var out Values`, set those same four defaults. YAML: if the key is absent or `isYAMLNull`, keep the default. If present, `capture` must be `!!bool` with value `true` or `false` (`ErrCapture` otherwise). `capture_empty` and `capture_https` are `!!str` and must be the legal enums. `capture_hosts` must be a `!!seq` of `!!str`; a scalar is `ErrCaptureHosts`. Trim each host, then call `capture.Compile(out.Capture, out.CaptureEmpty, out.CaptureHTTPS, hosts)` and return its error unchanged. Store the trimmed slice, using `[]string{}` when the sequence is empty.

JSON: same checks, but a missing key or JSON null is `ErrMissingField` via the existing required-key loop. Add the four names to that loop. A present non-bool `capture` returns `ErrCapture` before the missing-key loop. `capture_hosts` unmarshals as `[]string`; failure is `ErrCaptureHosts`. Then trim and `Compile`.

`MarshalYAML` appends, after `theme`, in this order:

```text
capture: true|false
capture_empty: "all"|"none"
capture_https: "decrypt"|"tunnel"
capture_hosts: []
```

A non-empty list is a YAML block sequence of `strconv.Quote` strings. Bool and enums are not quoted except the two string enums, which use `strconv.Quote`.

In `session.go` `SaveAndApply`, after the theme assignment, copy the four fields from `next` onto `s.runtime`. Clone hosts with `append([]string{}, next.CaptureHosts...)`. If that clone is empty, store `[]string{}`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/config/ ./internal/api/ -count=1`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/config/value.go internal/config/value_test.go internal/config/session.go internal/config/session_test.go internal/config/dir_test.go internal/api/config_test.go
git commit -m "feat: store the capture policy in config"
```

---

### Task 3: Config HTTP and the capture toggle

**Files:**
- Modify: `internal/api/config_http.go`
- Modify: `internal/api/server.go`
- Modify: `internal/api/config_test.go`
- Test: `internal/api/config_test.go`

**Interfaces:**
- Consumes: `config.Values` capture fields, `capture.Compile`, `proxy.SetPolicy` from Task 4. Add `SetPolicy` in this task first as a method that only stores a `capture.Policy`; Task 4 makes the proxy read it. Declare it in Task 4's file during this task so the API compiles: `func (p *Proxy) SetPolicy(policy capture.Policy)`.
- Produces: `PUT /api/capture`. Every successful `SaveAndApply` calls `proxy.SetPolicy` with `capture.Compile` of `applied.Runtime` inside the apply callback. A compile error panics; a parsed `Values` cannot fail `Compile`.

- [ ] **Step 1: Write the failing test**

Add `TestPutCapture` to `internal/api/config_test.go`. It opens a temp config the same way `TestConfigHTTP` does, creates the proxy, and calls `api.New(..., px)`.

```go
func TestPutCapture(t *testing.T) {
	dir, file, err := config.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	session := config.NewSession(dir, file, config.Overrides{}, file.Proxy, file.UI, file.MaxFlows, file.MaxBody)
	st := store.New(file.MaxFlows)
	px := proxy.New("127.0.0.1:0", nil, file.MaxBody, nil)
	srv := api.New(st, nil, session, px)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	res := putBody(t, ts.URL+"/api/capture", `{"capture":false}`)
	var snap struct {
		File    config.Values `json:"file"`
		Runtime config.Values `json:"runtime"`
	}
	decodeJSON(t, res, &snap)
	if snap.File.Capture || snap.Runtime.Capture {
		t.Fatal("capture still on")
	}
	if snap.File.Proxy != file.Proxy || snap.File.MaxFlows != file.MaxFlows {
		t.Fatalf("other fields changed: %+v", snap.File)
	}
	saved, err := os.ReadFile(filepath.Join(dir.Path(), "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(saved, []byte("capture: false")) {
		t.Fatalf("file: %s", saved)
	}

	res = putBody(t, ts.URL+"/api/capture", `{"capture":false,"theme":"light"}`)
	assertConfigError(t, res, http.StatusBadRequest, "配置含有未知字段")
	res = putBody(t, ts.URL+"/api/capture", `{}`)
	assertConfigError(t, res, http.StatusBadRequest, "配置缺少字段")
	res = putBody(t, ts.URL+"/api/capture", `{"capture":"false"}`)
	assertConfigError(t, res, http.StatusBadRequest, "抓包开关必须是布尔值")

	body := `{"proxy":"127.0.0.1:8080","ui":"127.0.0.1:8081","max_flows":1000,"max_body":1048576,"theme":"dark","capture":false,"capture_empty":"none","capture_https":"tunnel","capture_hosts":["*.bad"]}`
	res = putBody(t, ts.URL+"/api/config", body)
	assertConfigError(t, res, http.StatusBadRequest, "主机规则不合法")
	after, err := os.ReadFile(filepath.Join(dir.Path(), "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(saved, after) {
		t.Fatal("invalid save changed the file")
	}
}
```

Map the eight capture sentinels in `writeConfigError` before this test can pass. The test also needs `SetPolicy` to exist so the apply callback compiles.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/api/ -run TestPutCapture -count=1`

Expected: FAIL because `PUT /api/capture` is not routed.

- [ ] **Step 3: Implement the route and error map**

In `internal/proxy/proxy.go`, add the import `httpproxy/internal/capture` and:

```go
policy capture.Policy
```

inside `Proxy`, initialized in `New` to `capture.Default()`.

```go
func (p *Proxy) SetPolicy(policy capture.Policy) {
	p.mu.Lock()
	p.policy = policy
	p.mu.Unlock()
}
```

In `server.go` next to `PUT /api/config`:

```go
s.mux.HandleFunc("PUT /api/capture", s.putCapture)
```

In `config_http.go`, extend `writeConfigError`:

```go
case errors.Is(err, config.ErrCapture):
	status, msg = http.StatusBadRequest, "抓包开关必须是布尔值"
case errors.Is(err, config.ErrCaptureEmpty):
	status, msg = http.StatusBadRequest, "空规则必须是 all 或 none"
case errors.Is(err, config.ErrCaptureHTTPS):
	status, msg = http.StatusBadRequest, "未抓 HTTPS 必须是 decrypt 或 tunnel"
case errors.Is(err, config.ErrCaptureHosts):
	status, msg = http.StatusBadRequest, "主机规则必须是字符串列表"
case errors.Is(err, config.ErrCaptureHostsLimit):
	status, msg = http.StatusBadRequest, "主机规则最多 100 条"
case errors.Is(err, config.ErrCaptureHostLength):
	status, msg = http.StatusBadRequest, "主机规则不能超过 253 个字符"
case errors.Is(err, config.ErrCaptureHost):
	status, msg = http.StatusBadRequest, "主机规则不合法"
case errors.Is(err, config.ErrCaptureHostDup):
	status, msg = http.StatusBadRequest, "主机规则重复"
```

Add `applyCapture` and call it at the end of the existing `SaveAndApply` callback, including the max-flows and max-body branches:

```go
func (s *Server) applyCapture(v config.Values) {
	if s.proxy == nil {
		return
	}
	policy, err := capture.Compile(v.Capture, v.CaptureEmpty, v.CaptureHTTPS, v.CaptureHosts)
	if err != nil {
		panic(err)
	}
	s.proxy.SetPolicy(policy)
}
```

`putCapture` reads at most 1 MiB, decodes one JSON object into `map[string]json.RawMessage`, and rejects a second JSON value with `config.ErrJSON`. If `capture` is absent or null, return `config.ErrMissingField`. If any other key exists, return `config.ErrUnknownField`. If the value is not JSON `true` or `false`, return `config.ErrCapture`. Copy `session.Snapshot()` file values, set `Capture`, and `SaveAndApply` with the same callback `putConfig` uses. Respond with `{file, runtime}`.

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/api/ ./internal/proxy/ -count=1`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/api/config_http.go internal/api/server.go internal/api/config_test.go internal/proxy/proxy.go
git commit -m "feat: save capture from config and the top-bar toggle"
```

---

### Task 4: Proxy decisions

**Files:**
- Modify: `internal/proxy/proxy.go`
- Modify: `internal/proxy/http1.go`
- Modify: `internal/proxy/http2.go`
- Modify: `internal/proxy/body.go`
- Modify: `internal/proxy/protocol_test.go`
- Test: `internal/proxy/protocol_test.go`

**Interfaces:**
- Consumes: `capture.Policy`, `capture.Record`, `capture.DecryptOnly`, `capture.Tunnel`, `SetPolicy`
- Produces: CONNECT and plain HTTP behavior from Global Constraints. `newBodyCapture` increments `bodyCaptures atomic.Int64`. Tests in package `proxy` read it.

- [ ] **Step 1: Write the failing tests**

Add these tests to `internal/proxy/protocol_test.go`. Use `startProxy`, `newCA`, `memSink`, and `closedAddress` which already exist in this package.

```go
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
	before := bodyCaptures.Load()
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
	if bodyCaptures.Load() != before {
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
	pool := x509.NewCertPool()
	pool.AddCert(upstream.Certificate())
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
```

`httptest.NewTLSServer` puts its cert pool on `upstream.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs`. Use that pool and `ServerName: "127.0.0.1"`. Add a tiny `readerConn` in the test file:

```go
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
```

```go
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
	if created, updated := sink.counts(); created != 1 || updated != 1 {
		t.Fatalf("first sink %d %d", created, updated)
	}
	policy, err := capture.Compile(false, "all", "decrypt", nil)
	if err != nil {
		t.Fatal(err)
	}
	p.SetPolicy(policy)
	send()
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
	time.Sleep(100 * time.Millisecond)
	if created, updated := sink.counts(); created != 0 || updated != 0 {
		t.Fatalf("sink %d %d", created, updated)
	}
}
```

`dialMITM` fatals when CONNECT status is not 200. That is correct here: decrypt-only still returns 200 and then the handshake fails. Do not use `dialMITM` for the 502 tests.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/proxy/ -run 'TestDecryptOnlyForwardsWithoutCapture|TestTunnelPassesBytesWithoutMITM|TestTunnelDialFailureSends502Without200|TestTunnelRejectsSelf|TestPlainKeepAliveUsesTheNewPolicy|TestTLSFailureIsSilentWhenNotRecording' -count=1`

Expected: FAIL because every request is still recorded and CONNECT still performs MITM.

- [ ] **Step 3: Branch on the snapshot**

```go
func (p *Proxy) currentPolicy() capture.Policy {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.policy
}
```

`newBodyCapture` starts with `bodyCaptures.Add(1)`.

`handleHTTP1` passes `p.currentPolicy().Decide(false, req.Host) == capture.Record` into `handleRequest` for plain requests.

`handleConnect` reads the authority the way it does now, then:

```go
action := p.currentPolicy().Decide(true, connectAuthority)
if action == capture.Tunnel {
	p.spliceTunnel(conn, connectAuthority)
	return
}
record := action == capture.Record
```

The CA check and the existing MITM path run only after that. `recordTLSFailure` runs only when `record` is true. `handleHTTP2` and `handleRequest` receive `record`. `handleHTTP2Request` receives it too.

`spliceTunnel`:

```go
func (p *Proxy) spliceTunnel(conn net.Conn, authority string) {
	if _, _, err := net.SplitHostPort(authority); err != nil {
		writeResponse(conn, http.StatusBadGateway, "Bad Gateway\n")
		return
	}
	if p.isLoopbackTarget(&url.URL{Host: authority}) {
		writeResponse(conn, http.StatusBadGateway, "refusing to proxy to the proxy's own listen address\n")
		return
	}
	upstream, err := (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).Dial("tcp", authority)
	if err != nil {
		writeResponse(conn, http.StatusBadGateway, "Bad Gateway\n")
		return
	}
	if _, err := io.WriteString(conn, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		_ = upstream.Close()
		return
	}
	_ = conn.SetDeadline(time.Time{})
	_ = upstream.SetDeadline(time.Time{})
	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(upstream, conn)
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(conn, upstream)
		done <- struct{}{}
	}()
	<-done
	_ = upstream.Close()
	_ = conn.Close()
}
```

`handleRequest` signature gains `record bool`. When `record` is false, do not call `startFlow`, do not call `newBodyCapture`, and do not call `finishRecord` or `failUpstream`. Still write the upstream response, and on an upstream error still send `Bad Gateway\n`. Return `writeErr == nil && !closeAfter` in both modes so a plain keep-alive connection can send the next request. When `record` is true, keep the current body capture.

`failUpstream` and `finishRecord` return immediately when `rec == nil`. `tunnelWebSocket` checks `rec != nil` before writing `rec.f`.

`handleHTTP2Request` uses the same `record` flag. The `record == false` path round-trips and writes the response without `startFlow` or `newBodyCapture`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/proxy/ -count=1`

Expected: PASS, including the existing capture tests that never call `SetPolicy`.

- [ ] **Step 5: Commit**

```bash
git add internal/proxy/proxy.go internal/proxy/http1.go internal/proxy/http2.go internal/proxy/body.go internal/proxy/protocol_test.go
git commit -m "feat: skip capture and tunnel unmatched HTTPS"
```

Only stage the capture edits in `http2.go`. Do not stage unrelated hunks. If `git diff internal/proxy/http2.go` shows both, use `git apply` of a patch limited to the `record` parameter, or `git add -p` is unavailable here, so put the capture edit in a way that the pre-existing hunk stays unstaged. Inspect `git diff` before committing and do not include the unrelated change.

---

### Task 5: Wire startup

**Files:**
- Modify: `cmd/httpproxy/main.go`

**Interfaces:**
- Consumes: `capture.Compile`, `proxy.SetPolicy`, `config.Values` capture fields
- Produces: the listening proxy uses the loaded runtime policy

- [ ] **Step 1: Set the policy before accept**

In `cmd/httpproxy/main.go`, after `proxy.New` and before `http.ListenAndServe` / `proxyServer.ListenAndServe`:

```go
policy, err := capture.Compile(runtime.Capture, runtime.CaptureEmpty, runtime.CaptureHTTPS, runtime.CaptureHosts)
if err != nil {
	fmt.Fprintf(os.Stderr, "config: %v\n", err)
	return 1
}
proxyServer.SetPolicy(policy)
```

Do not add a flag.

- [ ] **Step 2: Run the tests**

Run: `go test ./cmd/httpproxy/ ./internal/config/ -count=1`

Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add cmd/httpproxy/main.go
git commit -m "feat: apply the saved capture policy at startup"
```

---

### Task 6: Summary and settings payload

**Files:**
- Create: `web/src/capture.ts`
- Create: `web/capture.test.ts`
- Modify: `web/package.json`
- Test: `web/capture.test.ts`

**Interfaces:**
- Consumes: `ConfigValues` from `web/src/types.ts` after this task adds the four fields
- Produces:
  - `export function captureSummary(file: ConfigValues | null): { text: string; title: string }`
  - `export function capturePayload(file: ConfigValues, empty: 'all' | 'none', httpsMode: 'decrypt' | 'tunnel', hostsText: string): ConfigValues`

- [ ] **Step 1: Write the failing test**

Create `web/capture.test.ts`:

```ts
import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { capturePayload, captureSummary } from './src/capture.ts'
import type { ConfigValues } from './src/types.ts'

const file: ConfigValues = {
  proxy: '127.0.0.1:8080',
  ui: '127.0.0.1:8081',
  max_flows: 1000,
  max_body: 1048576,
  theme: 'dark',
  capture: false,
  capture_empty: 'all',
  capture_https: 'decrypt',
  capture_hosts: ['example.com', '*.example.com', 'a.b.example.com', 'example.com:8443'],
}

describe('captureSummary', () => {
  it('says the config is loading', () => {
    assert.deepEqual(captureSummary(null), { text: '正在读取配置', title: '' })
  })

  it('joins the paused prefix, three sections, and the total count', () => {
    const got = captureSummary(file)
    assert.equal(got.text, '抓包已暂停 · 空规则：抓全部 · 未抓 HTTPS：解密 · 主机：example.com、*.example.com、a.b.example.com 等 4 条')
    assert.equal(got.title, 'example.com\n*.example.com\na.b.example.com\nexample.com:8443')
  })

  it('drops the paused prefix and writes 无 when there are no hosts', () => {
    const got = captureSummary({ ...file, capture: true, capture_empty: 'none', capture_https: 'tunnel', capture_hosts: [] })
    assert.equal(got.text, '空规则：不抓 · 未抓 HTTPS：隧道 · 主机：无')
    assert.equal(got.title, '无')
  })
})

describe('capturePayload', () => {
  it('keeps capture and drops blank lines', () => {
    const got = capturePayload(file, 'none', 'tunnel', ' Example.com \n\n *.example.com \n')
    assert.equal(got.capture, false)
    assert.equal(got.capture_empty, 'none')
    assert.equal(got.capture_https, 'tunnel')
    assert.deepEqual(got.capture_hosts, ['Example.com', '*.example.com'])
    assert.equal(got.proxy, file.proxy)
  })
})
```

Add the four fields to `ConfigValues` in `web/src/types.ts`:

```ts
capture: boolean
capture_empty: 'all' | 'none'
capture_https: 'decrypt' | 'tunnel'
capture_hosts: string[]
```

Change `web/package.json` `"test"` to:

```json
"test": "node --experimental-strip-types --test columns.test.ts settings.test.ts capture.test.ts"
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd web && node --experimental-strip-types --test capture.test.ts`

Expected: FAIL because `./src/capture.ts` does not exist.

- [ ] **Step 3: Implement the two functions**

Create `web/src/capture.ts` with `captureSummary` and `capturePayload` matching the tests. `capturePayload` copies `file` and replaces `capture_empty`, `capture_https`, and `capture_hosts`. It does not change `capture`. Split hosts on `\n`, trim, and drop empty lines.

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd web && npm test`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add web/src/capture.ts web/src/types.ts web/capture.test.ts web/package.json
git commit -m "feat: format the capture summary and settings payload"
```

---

### Task 7: Capture UI

**Files:**
- Modify: `web/src/api.ts`
- Modify: `web/src/components/TopBar.tsx`
- Modify: `web/src/components/SettingsPane.tsx`
- Modify: `web/src/App.tsx`
- Modify: `web/src/styles.css`
- Modify: `README.md`

**Interfaces:**
- Consumes: `captureSummary`, `capturePayload`, `putConfig`, `PUT /api/capture`
- Produces: the top-bar button, the read-only summary, and the three settings rows

- [ ] **Step 1: Add `putCapture`**

In `web/src/api.ts`:

```ts
export async function putCapture(capture: boolean): Promise<ConfigSnapshot> {
  const r = await fetch('/api/capture', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ capture }),
  })
  if (!r.ok) {
    let message = '无法写入配置'
    try {
      const body: { error?: unknown } = await r.json()
      if (typeof body.error === 'string') message = body.error
    } catch {
      /* body has no error string */
    }
    throw new Error(message)
  }
  return r.json()
}
```

- [ ] **Step 2: Replace `TopBar`**

`TopBar` props are `onClear`, `count`, `proxy`, `capture: boolean | null`, `disabled`, `error`, `onToggle`. Render the existing title, proxy hint, spacer, and count. Before `下载 CA`, render:

```tsx
<button className="btn" type="button" disabled={disabled || capture == null} onClick={onToggle}>
  {capture ? '暂停抓包' : '开始抓包'}
</button>
{error ? <span className="topbar-error">{error}</span> : null}
```

When `capture` is null, the label is `开始抓包` and the button is disabled.

- [ ] **Step 3: Extend the settings form**

`SettingsPane` gains `writeLocked: boolean`. Keep the existing rows. Below the body-limit row add:

- `空规则` with buttons `抓全部` (`all`) and `不抓` (`none`), using the existing `seg-btn` / `active` classes.
- `未抓 HTTPS` with buttons `解密` (`decrypt`) and `隧道` (`tunnel`).
- `主机规则` textarea, one rule per line, placeholder exactly these three lines: `example.com`, `*.example.com`, `example.com:8443`. Under it, `<span className="settings-note">保存后立即生效</span>`.

Initialize and refresh `capture_empty`, `capture_https`, and the textarea only from `snapshot.file.capture_empty`, `snapshot.file.capture_https`, and `snapshot.file.capture_hosts.join('\n')`. Do not put `capture` in that effect.

`onSubmit` calls `putConfig(capturePayload(snapshot.file, empty, httpsMode, hostsText))`. The submit button is disabled when `saving` or `writeLocked` is true. The top bar is disabled while this submit runs because `App` holds the lock.

- [ ] **Step 4: Wire the lock and the summary in `App`**

```tsx
const writing = useRef(false)
const [writeLocked, setWriteLocked] = useState(false)
const [toggleError, setToggleError] = useState('')
```

`beginWrite` returns false when `writing.current` is true; otherwise it sets the ref and `writeLocked`. `endWrite` clears both. `onToggle` calls `beginWrite`, then `putCapture(!(snapshot?.file.capture ?? false))`, then `onSaved`, and on failure sets `toggleError` to `err.message` without changing `capture`. Success clears `toggleError`.

Between `.filterbar` and `.workspace`:

```tsx
<p className="capture-summary" title={captureSummary(snapshot?.file ?? null).title}>
  {captureSummary(snapshot?.file ?? null).text}
</p>
```

Pass `writeLocked` to `SettingsPane`. Pass `disabled={writeLocked}` to `TopBar`.

Add to `web/src/styles.css`:

```css
.capture-summary {
  margin: 0;
  padding: 4px 12px 0;
  color: var(--text-muted);
  font-size: 12px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.topbar-error { color: var(--danger); }
```

- [ ] **Step 5: Document the defaults**

In `README.md`, replace the sentence that lists the new `config.yaml` keys so it also lists `capture: true`, `capture_empty: all`, `capture_https: decrypt`, and `capture_hosts: []`, in that order, after `theme: dark`.

- [ ] **Step 6: Verify**

Run: `cd web && npm test && npx tsc -b --pretty false`

Run: `go test ./... -count=1`

Expected: PASS. `tsc` prints no errors.

- [ ] **Step 7: Commit**

```bash
git add web/src/api.ts web/src/components/TopBar.tsx web/src/components/SettingsPane.tsx web/src/App.tsx web/src/styles.css README.md
git commit -m "feat: toggle capture and edit host rules from the UI"
```

---

## Self-review

Spec coverage: host matching and the three actions are Task 1. YAML defaults, JSON required fields, marshal order, and session runtime copies are Task 2. Chinese API errors, full save, and `PUT /api/capture` are Task 3. CONNECT, tunnel, decrypt-only, keep-alive, TLS failure, and body-capture counting are Task 4. Startup `SetPolicy` is Task 5. Summary and payload tests are Task 6. The button, summary row, settings rows, write lock, and README are Task 7. No WebSocket event or CLI flag is added.

Placeholder scan: every test in Tasks 1 and 4 has its source in the task. No step defers a behavior to "handle edge cases".

Type consistency: `Compile(capture bool, empty, httpsMode string, hosts []string) (Policy, error)` and `Decide(https bool, authority string) Action` are the same in Tasks 1 through 5. `SetPolicy(capture.Policy)` is introduced in Task 3 and used in Tasks 4 and 5. `captureSummary` and `capturePayload` match Tasks 6 and 7. Config JSON tags match the UI `ConfigValues` fields.
