# Task 6 Report: HTTP/2 MITM path

## Implemented

- Wired negotiated ALPN `h2` connections into `http2.Server.ServeConn`.
- Added per-stream flow capture with HTTP version `2`, bounded request/response bodies, and `OnCreated` before upstream forwarding.
- Pinned upstream dialing and recorded flow host to the original CONNECT authority.
- Forwarded upstream through `http2.Transport` with compression disabled and injectable upstream TLS settings.
- Added an end-to-end HTTP/2-over-CONNECT test that verifies both client and upstream HTTP/2 behavior.
- Added `golang.org/x/net/http2` at a Go 1.24-compatible version.

## Verification

- Red: `TestProxyHTTP2` failed with a connection reset at the unimplemented ALPN branch.
- Green: `go test ./internal/proxy/ -run TestProxyHTTP2 -v`
- Regression: `go test ./internal/proxy/ -v` and `go test ./...`
- Static analysis: `go vet ./...`
- Race detection: `go test -race ./...`
- IDE lint diagnostics and `git diff --check`: clean.
