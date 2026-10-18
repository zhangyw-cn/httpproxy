# httpproxy

`httpproxy` is a local, view-only HTTP/HTTPS man-in-the-middle (MITM)
debugging proxy. It records requests and responses in memory and displays them
in an embedded web UI. It does not modify, replay, or persist captured traffic.

> HTTPS inspection requires trusting a locally generated CA certificate. Only
> install that CA on development machines you control. The CA private key is
> stored in the CA directory and must not be shared.

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
- `make test` — run all Go tests
- `make dev-web` — start the Vite development server

## Run

```sh
./bin/httpproxy
```

By default, the proxy listens on `localhost:8080` and the web UI is available
at <http://localhost:8081>. At first startup, the CA certificate and private
key are generated under `~/.httpproxy`.

Point the HTTP and HTTPS proxy settings of your browser, application, or
operating system to `localhost:8080`, then open the UI. Applications that do
not use the configured proxy will not be captured.

For a command-line smoke test:

```sh
curl -x http://127.0.0.1:8080 http://example.com
```

## Install the CA certificate

Download the CA from the **CA certificate** link in the UI, or use
`~/.httpproxy/ca.crt` directly. Import it as a trusted root certificate using
your operating system or browser certificate manager:

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

| Flag | Default | Description |
| --- | --- | --- |
| `-proxy` | `127.0.0.1:8080` | Proxy listen address (loopback by default) |
| `-ui` | `127.0.0.1:8081` | Web UI listen address (loopback by default; Host allowlist applied) |
| `-max-flows` | `1000` | Maximum number of flows retained in memory |
| `-max-body` | `1048576` | Maximum stored bytes per request/response body (and decoded UI copy) |
| `-ca-dir` | `~/.httpproxy` | Directory containing the generated CA certificate and key |

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

- **Memory:** each retained flow may keep up to three `max-body`-sized buffers
  (request, raw response, decoded response). Worst case ≈ `max-flows × 3 × max-body`.
- **WebSocket:** cleartext/HTTPS Upgrade is tunneled when the upstream returns
  `101 Switching Protocols`; frame-level capture is not implemented.
- **Self-proxy:** requests whose target is the proxy's own listen address are refused.
