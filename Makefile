.PHONY: build web test dev-web

# Builds the React UI into internal/ui/dist (embedded via go:embed), then the Go binary.
build: web
	go build -o bin/httpproxy ./cmd/httpproxy/

web:
	cd web && npm ci && npm run build

dev-web:
	cd web && npm run dev

test:
	go test ./...
