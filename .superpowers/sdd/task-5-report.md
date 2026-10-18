# Task 5 Report: HTTPS MITM (HTTP/1.1 after TLS)

## Implemented

- Replaced CONNECT 501 handling with `200 Connection Established` and a TLS server backed by the local CA.
- Advertised ALPN protocols `h2` and `http/1.1`; HTTP/2 currently closes at an explicit Task 6 TODO.
- Added an HTTP/1.1 request loop after TLS, forwarding with `https` scheme and capturing flows through the existing sink lifecycle.
- Added optional `Proxy.UpstreamTLS`; nil retains production system roots, while tests inject an insecure config for `httptest` TLS.
- Preserved upstream `DisableCompression` behavior.
- Added IP SAN generation so MITM certificates validate for IP-based CONNECT targets.

## Verification

- Red: `TestProxyHTTPSMITM` failed with CONNECT `501 Not Implemented`.
- Green: `go test ./internal/proxy/ -run TestProxyHTTPSMITM -v`
- Regression: `go test ./...`
- Static analysis: `go vet ./...`
- Race detection: `go test -race ./...`
- IDE lint diagnostics: none.

## Important Finding Fix

- CONNECT authority is now the immutable upstream target for intercepted HTTPS requests; inner absolute URI and `Host` values cannot redirect the proxy.
- `Flow.Host` records the CONNECT authority, while path and query remain sourced from the inner request.
- Added `TestProxyHTTPSMITMUsesConnectAuthorityOverInnerHost` with a non-default upstream port and conflicting inner `Host`.
- Red: the regression test returned `502 Bad Gateway` because the inner `Host` was used for dialing.
- Green: `go test ./internal/proxy/ -v`
- Race detection: `go test -race ./internal/proxy/ -v`
