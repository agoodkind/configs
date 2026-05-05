# Lint is centralized in go-makefile. Do NOT define project-local lint,
# deadcode, audit, fmt, vet, or staticcheck targets here. They duplicate
# the central pipeline and let agents bypass strict rules. Run `make help`
# for the canonical entry points (build/check/lint/fmt) and per-linter
# sub-targets (lint-golangci, lint-format, lint-gocyclo, lint-deadcode,
# staticcheck-extra). Refresh baselines via the matching *-baseline target.
#
# mwan Makefile.
# Build/lint pipeline lives in go-makefile and is fetched at runtime.
# Project-local: protobuf codegen, multi-platform cross-builds for the
# secondary mwan-opnsense* binaries, govulncheck wrapper that allowlists a
# specific known gobgp CVE.

GO_MK_URL     := https://raw.githubusercontent.com/agoodkind/go-makefile/main/go.mk
GO_MK_API_URL := https://api.github.com/repos/agoodkind/go-makefile/contents/go.mk?ref=main
GO_MK         := .make/go.mk
GO_MK_CACHE   := $(or $(XDG_CACHE_HOME),$(HOME)/.cache)/go-makefile/go.mk
# Dev override: GO_MK_DEV_DIR=$HOME/Sites/go-makefile to iterate locally.
GO_MK_DEV_DIR ?=

# Identity. mwan's internal/version pkg uses lowercase unexported fields
# (gitCommit, gitDirty), so VPKG canonical stamping is skipped. Pre-populate
# GO_BUILD_LDFLAGS BEFORE -include $(GO_MK); go-build.mk's ?= preserves it.
BINARY   := mwan
CMD      := ./cmd/$(BINARY)
DIST_DIR := bin

# Build flags applied to every binary (host + cross-compile via build_platform).
export CGO_ENABLED   := 0
GO_BUILD_EXTRA_FLAGS := -trimpath -buildvcs=false

MWAN_VERSION_PKG := goodkind.io/mwan/internal/version
GIT_COMMIT       := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
GIT_DIRTY        := $(shell git diff --quiet HEAD -- . 2>/dev/null && echo clean || echo dirty)
GO_BUILD_LDFLAGS := \
	-X $(MWAN_VERSION_PKG).gitCommit=$(GIT_COMMIT) \
	-X $(MWAN_VERSION_PKG).gitDirty=$(GIT_DIRTY)

# Pipeline modules. Skip go-release.mk: mwan ships via the OPNsense plugin
# packaging path and via rsync deploy-live, not GoReleaser.
GO_MK_MODULES := go-build.mk

GO_MK_BOOTSTRAP := $(shell \
	mkdir -p "$(dir $(GO_MK))" "$(dir $(GO_MK_CACHE))"; \
	if [ -n "$(GO_MK_DEV_DIR)" ] && [ -f "$(GO_MK_DEV_DIR)/go.mk" ]; then \
		cp "$(GO_MK_DEV_DIR)/go.mk" "$(GO_MK)"; \
		printf '%s\n' "go.mk: using dev override $(GO_MK_DEV_DIR)/go.mk" >&2; \
	else \
		tmp="$(GO_MK).tmp"; \
		if curl -fsSL -H "Accept: application/vnd.github.raw" --connect-timeout 5 --max-time 10 "$(GO_MK_API_URL)" -o "$$tmp" || curl -fsSL --connect-timeout 5 --max-time 10 "$(GO_MK_URL)?v=$$(date +%s)" -o "$$tmp" || curl -fsSL --connect-timeout 5 --max-time 10 "$(GO_MK_URL)" -o "$$tmp"; then \
			mv "$$tmp" "$(GO_MK)"; \
			cp "$(GO_MK)" "$(GO_MK_CACHE)"; \
		elif [ -f "$(GO_MK_CACHE)" ]; then \
			rm -f "$$tmp"; \
			cp "$(GO_MK_CACHE)" "$(GO_MK)"; \
		elif [ ! -f "$(GO_MK)" ]; then \
			rm -f "$$tmp"; \
			printf '%s\n' "error: go.mk fetch failed and no cache available" >&2; \
		fi; \
	fi)

$(GO_MK):
	@mkdir -p $(dir $@)
	@if [ -n "$(GO_MK_DEV_DIR)" ] && [ -f "$(GO_MK_DEV_DIR)/go.mk" ]; then \
		cp "$(GO_MK_DEV_DIR)/go.mk" "$@"; \
		echo "go.mk: using dev override $(GO_MK_DEV_DIR)/go.mk" >&2; \
	elif curl -fsSL -H "Accept: application/vnd.github.raw" --connect-timeout 5 --max-time 10 "$(GO_MK_API_URL)" -o "$@" || curl -fsSL --connect-timeout 5 --max-time 10 "$(GO_MK_URL)?v=$$(date +%s)" -o "$@" || curl -fsSL --connect-timeout 5 --max-time 10 "$(GO_MK_URL)" -o "$@"; then \
		mkdir -p "$(dir $(GO_MK_CACHE))" && cp "$@" "$(GO_MK_CACHE)"; \
	elif [ -f "$(GO_MK_CACHE)" ]; then \
		echo "warning: go.mk fetch failed, using cached version" >&2; \
		cp "$(GO_MK_CACHE)" "$@"; \
	else \
		echo "error: go.mk fetch failed and no cache available" >&2; \
		exit 1; \
	fi

-include $(GO_MK)

.DEFAULT_GOAL := check

# Augment canonical build with proto codegen as a prerequisite. The recipe
# from go-build.mk still applies; this just adds the proto dep.
build: proto

# ---------------------------------------------------------------------------
# Project-local
# ---------------------------------------------------------------------------

# Protobuf codegen requires protoc, protoc-gen-go, protoc-gen-go-grpc on PATH.
PROTOC ?= protoc
GOBIN  ?= $(shell go env GOPATH)/bin
export PATH := $(GOBIN):$(PATH)

HOST_GOOS   := $(shell go env GOOS)
HOST_GOARCH := $(shell go env GOARCH)

GOVULNCHECK_WRAPPER := ./tools/govulncheck.sh

.PHONY: proto build-linux build-mwan-opnsense build-mwan-opnsense-host build-mwan-opnsense-linux \
        tidy fix clean

proto:
	@mkdir -p gen/mwan/v1
	$(PROTOC) -I proto \
		--go_out=gen --go_opt=paths=source_relative \
		--go-grpc_out=gen --go-grpc_opt=paths=source_relative \
		mwan/v1/mwan.proto \
		mwan/v1/mwan_opnsense.proto

# Reusable cross-platform build template. Pulls GO_BUILD_LDFLAGS and
# GO_BUILD_EXTRA_FLAGS from the canonical pipeline so every binary, host
# and cross-compile, lands with the same flag set.
#
# $(1)=output binary name, $(2)=GOOS, $(3)=GOARCH, $(4)=cmd path
define build_platform
	@mkdir -p $(DIST_DIR)
	GOOS=$(2) GOARCH=$(3) CGO_ENABLED=0 go build $(GO_BUILD_EXTRA_FLAGS) \
		-ldflags='$(GO_BUILD_LDFLAGS)' \
		-o $(DIST_DIR)/$(1) $(4)
endef

# build-linux and build-mwan-opnsense* depend on `check`, which fans
# out to: build vet lint test govulncheck staticcheck-extra. Full lint
# pass is mandatory before any deploy artifact lands in $(DIST_DIR)/.
build-linux: check
	$(call build_platform,$(BINARY)-linux,linux,amd64,$(CMD))

build-mwan-opnsense: check
	$(call build_platform,mwan-opnsense,freebsd,amd64,./cmd/mwan-opnsense)

# Linux build of mwan-opnsense, used as the *client* binary on
# vault/suburban to dial the daemon over the qemu unix socket
# (virtio-serial OOB transport). Same binary, different platform.
build-mwan-opnsense-linux: check
	$(call build_platform,mwan-opnsense-linux,linux,amd64,./cmd/mwan-opnsense)

# Bridge daemon. Runs on the Proxmox host. Holds one persistent gRPC
# ClientConn to the qemu virtio-serial chardev unix socket and proxies
# probe RPCs onto it via HTTP/2 stream multiplex.
build-mwan-opnsense-host: check
	$(call build_platform,mwan-opnsense-host,linux,amd64,./cmd/mwan-opnsense-host)

tidy:
	go mod tidy

# Run golangci-lint with --fix using the central config.
fix:
	$(GOLANGCI_LINT) run --fix $(GOLANGCI_LINT_FLAGS) $(GOLANGCI_LINT_TARGETS)

# Override go.mk's govulncheck with mwan's wrapper, which knows about a
# specific known gobgp vulnerability (GO-2026-4736) that was patched in
# v4.3.0 and is filtered out of the report.
govulncheck:
	$(GOVULNCHECK_WRAPPER)

clean: clean-dist
