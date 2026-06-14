GOCACHE ?= /tmp/agent-observer-gocache
GOMODCACHE ?= /tmp/agent-observer-gomodcache

.PHONY: run test build

run:
	GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) go run ./cmd/agent-observer --debug

test:
	GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) go test ./...

build:
	GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) go build ./...
