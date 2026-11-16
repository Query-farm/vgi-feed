# vgi-feed Makefile
#
# A VGI worker (Go) that fetches and parses RSS / Atom / JSON feeds into DuckDB
# rows. Targets:
#
#   make build       Build the worker + mock-server binaries
#   make test-unit   Run the pure-Go unit/integration tests (httptest)
#   make test-sql    Run the haybarn-unittest SQL E2E against a local mock server
#   make test        test-unit + test-sql
#   make fmt         gofmt -w
#   make vet         go vet
#   make lint        golangci-lint (if installed) else vet
#   make check       lint + test (what CI runs)
#   make clean       Remove built binaries
#
# test-sql installs the pinned haybarn-unittest SQL runner into .tools (needs
# uv) and runs test/sql over every transport via ci/run-integration.sh:
#   make test-sql                    stdio, launch and http
#   make test-sql TRANSPORTS=launch  one transport

WORKER_BIN  := vgi-feed-worker
MOCK_BIN    := mockserver
WORKER_CMD  := ./cmd/vgi-feed-worker
MOCK_CMD    := ./cmd/mockserver

# Absolute path to the built worker (the VGI extension launches it via LOCATION).
WORKER_PATH := $(CURDIR)/$(WORKER_BIN)
MOCK_PATH   := $(CURDIR)/$(MOCK_BIN)

# The SQL test runner, pinned like the vgi-starter's (make tools).
HAYBARN_VERSION  ?= 2.0.0rc11
TOOLS            := $(CURDIR)/.tools
HAYBARN_UNITTEST := $(TOOLS)/bin/haybarn-unittest
TRANSPORTS       ?= stdio launch http

.PHONY: build test test-unit test-sql tools check fmt vet lint clean

build:
	go build -o $(WORKER_BIN) $(WORKER_CMD)
	go build -o $(MOCK_BIN) $(MOCK_CMD)

test: test-unit test-sql

check: lint test

test-unit:
	go test ./...

tools: ## Install the pinned SQL test runner into .tools
	@test -x $(HAYBARN_UNITTEST) || { uv venv -q --allow-existing $(TOOLS) && uv pip install -q -p $(TOOLS) haybarn-unittest==$(HAYBARN_VERSION); }

# SQL tests through DuckDB over each transport in TRANSPORTS (stdio, launch,
# http); ci/run-integration.sh starts the mock backend and the HTTP worker.
test-sql: build tools
	@set -e; for t in $(TRANSPORTS); do \
		WORKER=$(WORKER_PATH) HAYBARN_UNITTEST=$(HAYBARN_UNITTEST) TRANSPORT=$$t ci/run-integration.sh; \
	done

fmt:
	gofmt -w .

vet:
	go vet ./...

lint:
	@command -v golangci-lint >/dev/null 2>&1 || { \
		echo "golangci-lint not found. Install: https://golangci-lint.run/usage/install/"; \
		exit 1; \
	}
	golangci-lint run ./...

clean:
	rm -f $(WORKER_BIN) $(MOCK_BIN)
