GO_MK_URL   := https://raw.githubusercontent.com/agoodkind/go-makefile/main/go.mk
GO_MK       := .make/go.mk
GO_MK_CACHE := $(HOME)/.cache/go-makefile/go.mk

$(GO_MK):
	@mkdir -p $(dir $@)
	@if curl -fsSL --connect-timeout 5 --max-time 10 "$(GO_MK_URL)" -o "$@"; then \
		mkdir -p "$(dir $(GO_MK_CACHE))" && cp "$@" "$(GO_MK_CACHE)"; \
	elif [ -f "$(GO_MK_CACHE)" ]; then \
		echo "warning: go.mk fetch failed, using cached version" >&2; \
		cp "$(GO_MK_CACHE)" "$@"; \
	else \
		echo "error: go.mk fetch failed and no cache available" >&2; \
		exit 1; \
	fi

-include $(GO_MK)

.PHONY: update-go-mk
update-go-mk:
	@mkdir -p "$(dir $(GO_MK))"
	@if curl -fsSL --connect-timeout 5 --max-time 10 "$(GO_MK_URL)" -o "$(GO_MK)"; then \
		mkdir -p "$(dir $(GO_MK_CACHE))" && cp "$(GO_MK)" "$(GO_MK_CACHE)"; \
		echo "go.mk updated"; \
	else \
		echo "error: go.mk fetch failed" >&2; \
		exit 1; \
	fi

# Protobuf codegen requires protoc, protoc-gen-go, protoc-gen-go-grpc on PATH.
PROTOC ?= protoc
GOBIN ?= $(shell go env GOPATH)/bin
export PATH := $(GOBIN):$(PATH)

GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
GIT_DIRTY  := $(shell git diff --quiet HEAD -- . 2>/dev/null && echo clean || echo dirty)
LDFLAGS    := -X goodkind.io/mwan/internal/version.gitCommit=$(GIT_COMMIT) -X goodkind.io/mwan/internal/version.gitDirty=$(GIT_DIRTY)

BINARY := mwan
CMD    := ./cmd/$(BINARY)

.DEFAULT_GOAL := check

.PHONY: proto build build-linux clean tidy fmt fix

fmt:
	golangci-lint fmt ./...

fix:
	golangci-lint run --fix ./...

# go.mk's `staticcheck-extra` system uses its own bundled analyzer set by
# default (5 custom AST analyzers, installed via `go install` from
# go-makefile). Project just sets the per-finding baseline file.
# See STATICCHECK-NOTES.md for the documented exceptions.
STATICCHECK_EXTRA_BASELINE := .staticcheck-extra-baseline.txt

tidy:
	go mod tidy

proto:
	@mkdir -p gen/mwan/v1
	$(PROTOC) -I proto \
		--go_out=gen --go_opt=paths=source_relative \
		--go-grpc_out=gen --go-grpc_opt=paths=source_relative \
		mwan/v1/mwan.proto

build: proto
	@mkdir -p bin
	go build -trimpath -ldflags="$(LDFLAGS)" -o bin/$(BINARY) $(CMD)

build-linux: build vet lint test proto
	@mkdir -p bin
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o bin/$(BINARY)-linux $(CMD)

clean:
	rm -rf bin/
