.PHONY: build test

build:
	go build -o bin/httpproxy ./cmd/httpproxy/

test:
	go test ./...
