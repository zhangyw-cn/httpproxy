.PHONY: build web test dev-web help

# Build the embedded React UI and the Go binary in bin/.
build: web
	go build -o bin/httpproxy ./cmd/httpproxy/

# Install frontend dependencies and build the UI into internal/ui/dist.
web:
	cd web && npm ci && npm run build

# Start the Vite development server (run the Go backend separately).
dev-web:
	cd web && npm run dev

# Run the complete Go test suite.
test:
	go test ./...

# List the documented development targets.
help:
	@printf '%s\n' \
		'build    Build the embedded UI and bin/httpproxy' \
		'web      Install dependencies and build the embedded UI' \
		'dev-web  Start the Vite development server' \
		'test     Run all Go tests'
