# httpproxy

`httpproxy` is a local HTTP/HTTPS proxy with optional traffic capture and
per-profile hosts overrides. With capture off it is an ordinary forwarding
proxy: plain HTTP is forwarded untouched and HTTPS is tunneled end to end, with
no CA certificate needed. Turn capture on and it also acts as a view-only
man-in-the-middle debugging proxy that records requests and responses in memory
and displays them in an embedded web UI.

`httpproxy` never modifies requests or responses, and it does not replay or
persist captured traffic. Hosts overrides do change one thing: where the proxy
dials upstream. A matching domain is sent to the IP you configure, while the
original host name, `Host` header, TLS SNI, and certificate validation stay
unchanged.

> Capture is optional. When it is on, HTTPS inspection requires trusting a
> locally generated CA certificate. Only install that CA on development machines
> you control. The CA private key is stored in the CA directory and must not be
> shared.

## Requirements

- Go 1.24.4 or later
- Node.js and npm (to build or develop the web UI)

## Build

Build the React UI and the single Go executable:

```sh
make build
```

The executable is written to `bin/httpproxy`. To run the steps manually:

```sh
cd web
npm ci
npm run build
cd ..
go build -o bin/httpproxy ./cmd/httpproxy
```

Other Make targets:

- `make web` — install frontend dependencies and build the embedded UI
- `make test` — run all Go tests and the web UI unit tests
- `make dev-web` — start the Vite development server

## Run

```sh
./bin/httpproxy
./bin/httpproxy /path/to/workdir
```

By default, the proxy listens on `127.0.0.1:8080` and the web UI is available
at <http://127.0.0.1:8081>. The CA, `config.yaml`, and the hosts profile
directory are created under `<workdir>/.httpproxy/` on first launch. The workdir
is the optional directory argument, or the current directory when it is
omitted. `httpproxy` does not read `~/.httpproxy`.

```text
<workdir>/.httpproxy/      # mode 0700
  .gitignore               # "*" written only when this file is absent
  config.yaml              # mode 0644
  ca.crt
  ca.key                   # mode 0600
  hosts/                   # mode 0700, created on startup
    测试环境.hosts
    预发.hosts
```

`.gitignore` is written as `*` only when that file is absent. An existing
`.gitignore` is left unchanged. `ca.key` is mode `0600`. A new `config.yaml`
uses these defaults:

```yaml
proxy: "127.0.0.1:8080"
ui: "127.0.0.1:8081"
max_flows: 1000
max_body: 1048576
theme: dark
capture: false
capture_empty: all
capture_https: decrypt
capture_hosts: []
hosts_profile: ""
```

A new installation therefore starts as a plain proxy: no CA has to be trusted
and HTTPS works immediately. An existing `config.yaml` keeps the `capture` value
it already has. A missing `hosts_profile` is read as `""` (disabled) and the file
is not rewritten; upgrade launches also create an empty `.httpproxy/hosts/`
directory.

Point the HTTP and HTTPS proxy settings of your browser, application, or
operating system to `127.0.0.1:8080`, then open the UI. Applications that do
not use the configured proxy will not be captured.

For a command-line smoke test:

```sh
curl -x http://127.0.0.1:8080 http://example.com
```

## Panels

The UI has a narrow activity bar on the left with four entries: **首页** (home),
**抓包** (capture), **Hosts**, and a gear for **设置** (settings) at the bottom.
The old top bar is gone. Home is the default panel, and the last panel is not
remembered.

- **Home** shows the proxy address with a copy button, uptime, capture status
  and toggle, the active hosts profile with a quick switch, a CA download, and
  the client list.
- **Capture** holds the capture toggle, the record count, clear, the capture
  rules, and the flow table with filters and detail. Flow detail shows an
  **上游** (upstream) section only when a hosts override was applied: the
  original target, the address actually dialed, and the profile name.
- **Hosts** manages hosts profiles and lets you create, edit, rename, delete,
  activate, and deactivate them.
- **Settings** keeps only app-level values: theme, proxy address, UI address,
  retained flow count, and the response body limit. Capture rules are not here;
  they live in the capture panel.

## Capture

The `capture` setting is a single switch.

- **Capture off:** plain HTTP is still forwarded normally but not recorded.
  Every HTTPS `CONNECT` becomes a raw TCP tunnel: no leaf certificate is
  issued, no CA is needed, and no new flows appear. Existing flows are kept and
  can still be cleared. `capture_empty`, `capture_https`, and `capture_hosts`
  do not apply while capture is off.
- **Capture on:** HTTPS is decrypted for recorded requests (and for unrecorded
  ones when `capture_https` is `decrypt`), following the rules in the capture
  panel:
  - `capture_empty` — the empty-rule fallback (空规则), used only while
    `capture_hosts` is empty: `all` records every request and `none` records
    nothing. Once `capture_hosts` has one or more rules, only matching requests
    are recorded and `capture_empty` no longer applies.
  - `capture_hosts` — domain rules that decide what is recorded. Rules use the
    original request authority and are not affected by hosts overrides.
  - `capture_https` — what happens to HTTPS that is not recorded: `decrypt`
    inspects and forwards it without recording, `tunnel` forwards it as opaque
    TLS.
- **Pause:** pausing is turning capture off. New records stop immediately, even
  for requests arriving on an already-decrypted connection; forwarding
  continues, and new `CONNECT` requests are tunneled. Resuming applies the
  current rules to subsequent requests. Pausing does not clear existing flows.

The capture switch is available on both Home and the capture panel and writes
`capture` to `config.yaml`. The CA is needed whenever capture is on and HTTPS is
decrypted, including HTTPS that is inspected but not recorded. It is not needed
when capture is off, or for unmatched hosts when `capture_https` is `tunnel`.
With capture off, clients see the origin certificate and must trust that
instead.

## Hosts overrides

Hosts overrides work like a system hosts file, but only for traffic through the
proxy. Each profile is one UTF-8 file under `.httpproxy/hosts/`; the file name
minus `.hosts` is the profile name. The active profile is stored in
`config.yaml` as `hosts_profile`; an empty string disables overrides.

```text
.httpproxy/
  config.yaml              # hosts_profile: "测试环境"
  hosts/
    测试环境.hosts
    预发.hosts
```

A profile file uses the same shape as `/etc/hosts`:

```text
# local integration targets
10.0.0.5   api.example.com
10.0.0.6   cdn.example.com static.example.com
::1        v6.example.com
```

- Each line is `<IP> <domain> [domain...]`, separated by whitespace. Blank
  lines are ignored and `#` starts a comment.
- IPs must be IPv4 or IPv6 literals. Domains are matched exactly and
  case-insensitively; ports and wildcards are not accepted.
- A file may contain at most 1 MiB and 1000 mapping lines.
- If the same domain appears more than once, the **first** mapping wins. Later
  duplicates are ignored with a non-blocking warning in the Hosts panel rather
  than overriding the first one.
- Syntax errors are reported with a line number and are never saved.

Activation can happen through the Hosts panel or the dropdown on Home, and is
written to `hosts_profile` in `config.yaml`. Changes take effect immediately for
new connections: the proxy swaps the lookup table and closes idle upstream
connections so the next request dials the new address. Requests already in
flight and established tunnels are not interrupted and keep using the old IP.

Only the upstream dial address changes. For `http://api.example.com:8080`, a
mapping to `10.0.0.5` dials `10.0.0.5:8080`; the port always comes from the
request, never from the hosts file. For HTTPS, `CONNECT api.example.com:443`
dials the mapped IP on port 443, but TLS SNI, certificate validation, and the
`Host` header all remain `api.example.com`. If the mapped address is the proxy's
own listen address, the request is refused with `502` to avoid a loop.

A profile that is active at startup must exist and parse; otherwise the process
exits with a `hosts:` error instead of silently falling back to real DNS. If the
active file is edited into invalid content while running, the last good rules
keep working and the Home and Hosts panels show a warning.

## Client sources

Home lists clients aggregated by source IP, independent of capture. Each row
shows active connections, requests, HTTPS tunnels, last activity, and the most
recent hosts visited. Client statistics are tracked per-IP in memory only: they
are reset when the process restarts, idle entries are dropped after five
minutes, and at most 200 clients are shown. Clearing flows does not clear them.

The list refreshes every two seconds while Home is visible. Because the default
listeners are loopback-only, a local setup shows one row, `127.0.0.1`; a second
machine appears only when you bind the proxy to a LAN address. There is no
process attribution — `httpproxy` cannot tell which application made a request.

## Install the CA certificate

The CA is only needed for capture-on HTTPS inspection. Download it from the
**下载 CA** button on Home, or use `<workdir>/.httpproxy/ca.crt` directly.
Import it as a trusted root certificate using your operating system or browser
certificate manager:

- **Linux:** add it to the distribution's local CA directory and refresh the
  trust store (for example, copy it to `/usr/local/share/ca-certificates/` and
  run `sudo update-ca-certificates` on Debian/Ubuntu).
- **macOS:** import it into the System keychain with Keychain Access and set it
  to **Always Trust**.
- **Windows:** import it into **Trusted Root Certification Authorities** for
  the current user or local machine.
- **Firefox:** if Firefox is not using the system trust store, import it under
  Settings → Privacy & Security → Certificates → Authorities.

Restart affected applications after installation. Remove the certificate from
the trust store when it is no longer needed.

## Command-line flags

`DIR` selects the workdir. It is not a config override. The four flags below
override the file for this process only. Settings edits are written to
`.httpproxy/config.yaml` and do not copy those flags back into the file.
`-proxy` and `-ui` apply only after a restart when changed from the settings
page. Without `-max-flows` or `-max-body`, a saved limit takes effect in this
process. Theme takes effect immediately. There is no command-line override for
hosts profiles; use the Hosts panel or the Home dropdown.

| Argument | Description |
| --- | --- |
| `DIR` | Optional positional workdir. Omit it to use the current directory. |
| `-proxy` | This-process override of the proxy listen address. |
| `-ui` | This-process override of the web UI listen address. |
| `-max-flows` | This-process override of the retained flow count. |
| `-max-body` | This-process override of the maximum stored bytes per request/response body (and decoded UI copy). |

The gear at the bottom of the left bar opens settings for theme, proxy address,
UI address, retained flow count, and body limit. The CA download is on Home;
clear is in the capture panel.

Example (LAN access — only if you accept that decrypted traffic is exposed):

```sh
./bin/httpproxy -proxy 0.0.0.0:8080 -ui 0.0.0.0:8081 \
  -max-flows 2000 -max-body 2097152
```

The UI rejects non-loopback `Host` headers unless you also allow the listen
host via the built-in allowlist (loopback plus the `-ui` bind host). Prefer
keeping the default loopback bind for day-to-day use.

## Development mode

Run the Go backend in one terminal:

```sh
go run ./cmd/httpproxy
```

Run Vite in another terminal:

```sh
cd web
npm install
npm run dev
```

Open the URL printed by Vite (normally <http://localhost:5173>). Vite proxies
`/api` and `/ws` to the Go UI server on `127.0.0.1:8081`.

## Limitations

Certificate-pinned and mutual-TLS applications generally cannot be inspected.
HTTP/3, SOCKS, transparent proxying, upstream proxy chaining, request
modification, replay, and persistent capture storage are outside the v1 scope.

Additional v1 notes:

- **Hosts matching:** overrides are exact domain-to-IP mappings only. There are
  no wildcards, regular expressions, port mappings, aliases, or automatic
  import of the system `/etc/hosts`. The upstream port always comes from the
  request. Capture rules (`capture_hosts`) are separate and support their own
  simple wildcard/port syntax.
- **Client statistics:** collected per-IP only, kept in memory, and reset on
  restart. There is no process attribution, byte accounting, history, or
  charting.
- **Memory:** each retained flow may keep up to three `max-body`-sized buffers
  (request, raw response, decoded response). Worst case ≈ `max-flows × 3 × max-body`.
- **WebSocket:** cleartext/HTTPS Upgrade is tunneled when the upstream returns
  `101 Switching Protocols`; frame-level capture is not implemented.
- **Self-proxy:** requests whose target is the proxy's own listen address are refused.
