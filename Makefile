GOCACHE ?= /tmp/agent-observer-gocache
GOMODCACHE ?= /tmp/agent-observer-gomodcache
VERSION ?= dev
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -X github.com/techsavd/agent-observer/internal/app.Version=$(VERSION) -X github.com/techsavd/agent-observer/internal/app.Commit=$(COMMIT) -X github.com/techsavd/agent-observer/internal/app.BuildDate=$(DATE)

.PHONY: run test race vet build check snapshot smoke install-snapshot sign-macos doctor-fixture dump-fixture dump-active-fixture dump-warnings-fixture

run:
	GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) go run -ldflags "$(LDFLAGS)" ./cmd/agent-observer --debug

test:
	GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) go test ./...

race:
	GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) go test -race ./...

vet:
	GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) go vet ./...

build:
	GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) go build -ldflags "$(LDFLAGS)" ./...

check: test race vet build

snapshot:
	GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) goreleaser release --snapshot --clean

smoke:
	scripts/smoke-test

install-snapshot:
	scripts/install-snapshot

sign-macos:
	scripts/macos-sign-notarize dist

doctor-fixture:
	GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) go run -ldflags "$(LDFLAGS)" ./cmd/agent-observer doctor --redact --tasks-dir ./internal/testdata/claude/tasks --teams-dir ./internal/testdata/claude/missing-teams

dump-fixture:
	GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) go run -ldflags "$(LDFLAGS)" ./cmd/agent-observer --dump-text --tasks-dir ./internal/testdata/claude/tasks --teams-dir ./internal/testdata/claude/missing-teams

dump-active-fixture:
	GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) go run -ldflags "$(LDFLAGS)" ./cmd/agent-observer --dump-text --focus active --tasks-dir ./internal/testdata/claude/tasks --teams-dir ./internal/testdata/claude/missing-teams

dump-warnings-fixture:
	GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) go run -ldflags "$(LDFLAGS)" ./cmd/agent-observer --dump-text --focus warnings --tasks-dir ./internal/testdata/claude/tasks --teams-dir ./internal/testdata/claude/missing-teams
