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

.PHONY: proto build build-linux build-mwan-opnsense build-mwan-opnsense-host build-mwan-opnsense-linux clean tidy fmt fix

GOVULNCHECK_WRAPPER := ./tools/govulncheck.sh

fmt:
	golangci-lint fmt ./...

fix:
	golangci-lint run --fix ./...

# go.mk's `staticcheck-extra` system defaults to the core analyzer set.
# This project opts into the bundled strict analyzer set as well, matching
# the strict go-makefile template and keeping the baseline as the only
# per-project exception list.
STATICCHECK_EXTRA_FLAGS = $(STATICCHECK_EXTRA_CORE_FLAGS) $(STATICCHECK_EXTRA_STRICT_FLAGS)
STATICCHECK_EXTRA_BASELINE := .staticcheck-extra-baseline.txt

tidy:
	go mod tidy

proto:
	@mkdir -p gen/mwan/v1
	$(PROTOC) -I proto \
		--go_out=gen --go_opt=paths=source_relative \
		--go-grpc_out=gen --go-grpc_opt=paths=source_relative \
		mwan/v1/mwan.proto \
		mwan/v1/mwan_opnsense.proto

# Reusable cross-platform build template. All build targets fold
# through this so the flag set is identical everywhere
# (CGO_ENABLED=0, -trimpath, -buildvcs=false, ldflags from version
# package).
#
# $(1)=output binary name, $(2)=GOOS, $(3)=GOARCH, $(4)=cmd path
define build_platform
	@mkdir -p bin
	GOOS=$(2) GOARCH=$(3) CGO_ENABLED=0 go build -trimpath \
		-ldflags="$(LDFLAGS)" \
		-buildvcs=false \
		-o bin/$(1) $(4)
endef

HOST_GOOS   := $(shell go env GOOS)
HOST_GOARCH := $(shell go env GOARCH)

# build is the host-native default mwan binary; depends on proto so
# go.mk's `check` recipe (which does `build vet lint test ...`) does
# not loop. NOTE: now produces a CGO_ENABLED=0 + -buildvcs=false
# binary like the cross-compile targets, for consistent flags.
build: proto
	$(call build_platform,$(BINARY),$(HOST_GOOS),$(HOST_GOARCH),$(CMD))

# build-linux and build-mwan-opnsense* depend on `check`, which fans
# out to: build vet lint test govulncheck staticcheck-extra (defined
# by go.mk). Full lint pass is mandatory before any deploy artifact
# lands in bin/.
build-linux: check
	$(call build_platform,$(BINARY)-linux,linux,amd64,$(CMD))

build-mwan-opnsense: check
	$(call build_platform,mwan-opnsense,freebsd,amd64,./cmd/mwan-opnsense)

# Host-native build of mwan-opnsense for local CLI use. Always lives at
# bin/mwan-opnsense-host so it never
# shadows the cross-compiled FreeBSD artifact at bin/mwan-opnsense.
build-mwan-opnsense-host: check
	$(call build_platform,mwan-opnsense-host,$(HOST_GOOS),$(HOST_GOARCH),./cmd/mwan-opnsense)

# Linux build of mwan-opnsense, used as the *client* binary on
# vault/suburban to dial the daemon over the qemu unix socket
# (virtio-serial OOB transport). Same binary, different platform.
build-mwan-opnsense-linux: check
	$(call build_platform,mwan-opnsense-linux,linux,amd64,./cmd/mwan-opnsense)

clean:
	rm -rf bin/

govulncheck:
	$(GOVULNCHECK_WRAPPER)
