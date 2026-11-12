SHELL := /bin/sh
GO_CACHE := $(CURDIR)/.cache/go-build
export GOCACHE := $(GO_CACHE)
.PHONY: dev build test lint run clean e2e test-http

dev: build
	@echo "Frontend: http://127.0.0.1:5173; backend: http://127.0.0.1:8080"
	node scripts/dev.mjs

build:
	npm run build
	go build -trimpath -o ai-workspace ./cmd/server

test:
	go test ./...
	go test -race ./...
	npm run typecheck
	npm run test:assistant

lint:
	go vet ./...
	npm run lint

run: build
	./ai-workspace

test-http:
	mkdir -p .cache
	go build -o .cache/httpbridge ./cmd/httpbridge
	node scripts/http-mcp-smoke.mjs

e2e: build
	npm run test:e2e

# Intentionally preserve SQLite, token, user files, dependencies and npm cache.
clean:
	@echo "Removing generated binary, frontend build and test reports (no workspace data)."
	rm -f ai-workspace
	rm -rf web/dist test-results playwright-report
	mkdir -p web/dist
	touch web/dist/.keep
