# Task 4 Report: Body reader and cleartext proxy

## Status

Complete. Added a bounded flow-body capture helper, the `FlowSink` API, and a cleartext HTTP/1.1 proxy with lifecycle methods and flow capture.

## Commit

- `11f4dae feat: add cleartext HTTP/1.1 proxy with flow capture`

## TDD evidence

Red:

```text
go test ./internal/proxy -run 'Test(ReadLimited|Proxy)' -v
undefined: readLimited
undefined: New
undefined: Proxy
FAIL httpproxy/internal/proxy [build failed]
```

Green:

```text
go test ./internal/proxy -v
PASS
ok httpproxy/internal/proxy 0.018s
```

Final verification:

```text
go test -race ./...
ok httpproxy/internal/cert
ok httpproxy/internal/flow
ok httpproxy/internal/proxy
ok httpproxy/internal/store

go vet ./...
# exit 0
```

IDE diagnostics reported no linter errors in `internal/proxy`.

## Requirements covered

- Added `FlowSink`, `New`, `ListenAndServe`, `Close`, and concurrency-safe `Addr`.
- Uses `github.com/google/uuid` for flow IDs.
- Proxies absolute-form cleartext HTTP requests and preserves full request/response bodies while retaining only `maxBody` bytes in flows.
- Captures request/response headers, sizes, truncation flags, timing, status, and client metadata.
- Records upstream failures via `OnUpdated` before returning HTTP 502.
- Returns HTTP 501 for CONNECT until Task 5.
- Closes the listener and active client connections cleanly.

## Concerns

- The Task 4 skeleton intentionally handles one HTTP/1.1 request per client connection; persistent connections and CONNECT MITM are deferred.
- Successful flows are finalized after the response body has been written (or drained after a client write failure), so slow clients affect `DurationMs`.

## Review fixes

- Disabled compression handling on the upstream transport so flows retain the wire-level response representation without adding `Accept-Encoding`.
- Request bodies are fully consumed before `OnCreated`; the created flow now includes bounded body bytes, total size, and truncation state, while the complete body is reconstructed for upstream forwarding.
- Stored HTTP versions now use the spec form (`1.1`).
- Added regression coverage for created-flow request capture, full forwarding after truncation, HTTP version formatting, and untouched compression.

Verification:

```text
go test ./internal/proxy/ -v
PASS
ok httpproxy/internal/proxy 0.011s

go test -race ./...
ok httpproxy/internal/cert
ok httpproxy/internal/flow
ok httpproxy/internal/proxy 1.022s
ok httpproxy/internal/store
```
