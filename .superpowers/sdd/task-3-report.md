# Task 3 Report: CA manager

## Status

Complete. Implemented persistent local CA creation/loading, dynamic hostname leaf certificates, CA PEM export, and synchronized leaf caching.

## Commit

- `cf20aa1 feat: add local CA manager with dynamic leaf certs`

## TDD evidence

Red:

```text
go test ./internal/cert/ -v
httpproxy/internal/cert: no non-test Go files
FAIL httpproxy/internal/cert [build failed]
```

Green:

```text
go test ./internal/cert/ -v
PASS
ok httpproxy/internal/cert 0.015s
```

Final verification:

```text
go test ./...
ok httpproxy/internal/cert
ok httpproxy/internal/flow
ok httpproxy/internal/store

go test -race ./internal/cert/
ok httpproxy/internal/cert 1.021s
```

IDE diagnostics reported no linter errors in either changed file.

## Requirements covered

- Creates the CA directory and persists `ca.crt` plus `ca.key`.
- Writes and reloads the CA private key with mode `0600`.
- Reuses the same CA certificate on subsequent `LoadOrCreate` calls.
- Generates approximately 24-hour server leaves signed by the local CA.
- Caches leaf certificates by hostname under a mutex.
- Sets each leaf's `DNSNames` to its requested hostname.
- Uses `localhost` when SNI is absent.
- Completes a real HTTP-over-TLS handshake when the client trusts `CACertPEM`.

## Concerns

- Leaf certificates and keys are intentionally memory-only and are regenerated after process restart.
- CA initialization locking relies on filesystem `flock` semantics.

## Review fixes

- CA initialization is serialized with a directory-local `flock`.
- Both PEM files are fully written and synced to same-directory temporary files before rename; publication failures remove any newly published partial pair.
- Existing incomplete pairs are rejected instead of silently regenerated.
- Loaded CA certificates are checked against the private key's public key.
- Added mismatch, incomplete-pair, and corrupt-PEM regression tests.

TDD red:

```text
TestLoadOrCreateRejectsMismatchedCertificateAndKey: LoadOrCreate error = <nil>
TestLoadOrCreateRejectsIncompletePair: LoadOrCreate error = <nil>
FAIL httpproxy/internal/cert
```

Review-fix verification:

```text
go test ./internal/cert/ -v
PASS
ok httpproxy/internal/cert 0.007s

go test -race ./internal/cert/ -v
PASS
ok httpproxy/internal/cert 1.023s
```

IDE diagnostics reported no linter errors in the changed Go files.
