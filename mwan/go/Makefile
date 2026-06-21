# Lint is centralized in go-makefile. Do NOT define project-local lint,
# deadcode, audit, fmt, vet, or staticcheck targets here. They duplicate
# the central pipeline and let agents bypass strict rules. Run `make help`
# for the canonical entry points (build/check/lint/fmt) and per-linter
# sub-targets (lint-golangci, lint-format, lint-gocyclo, lint-deadcode,
# staticcheck-extra). Refresh baselines via the matching *-baseline target.
#
# mwan Makefile.
# Build/lint pipeline lives in go-makefile and is fetched at runtime.
# Project-local: protobuf codegen, cross-builds for the Linux and FreeBSD
# monolith entrypoints, plus a govulncheck install spec that wraps a
# specific known gobgp advisory false positive.

# Identity. mwan's internal/version pkg uses lowercase unexported fields
# (gitCommit, gitDirty), so VPKG canonical stamping is skipped. Pre-populate
# GO_BUILD_LDFLAGS BEFORE -include $(GO_MK); go-build.mk's ?= preserves it.
BINARY   := mwan
CMD      := ./cmd/$(BINARY)
DIST_DIR := bin

# Build flags applied to every binary.
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
# Route go-mk's inline govulncheck to mwan's local wrapper so `make build` and
# `make govulncheck` share the same allowlist for the stale gobgp advisory.
GOVULNCHECK_INSTALL := ./tools/cmd/govulncheck

include bootstrap.mk

.DEFAULT_GOAL := check

# Acceptance gate. `make build` fails unless every shipped platform compiles
# cleanly. Add new platform targets to this prerequisite list when we start
# shipping them.
build: proto build-linux build-mwan-opnsense

# ---------------------------------------------------------------------------
# Project-local
# ---------------------------------------------------------------------------

# Protobuf codegen requires buf, protoc-gen-go, protoc-gen-go-grpc on PATH.
BUF    ?= buf
GOBIN  ?= $(shell go env GOPATH)/bin
export PATH := $(GOBIN):$(PATH)

HOST_GOOS   := $(shell go env GOOS)
HOST_GOARCH := $(shell go env GOARCH)

.PHONY: proto build-linux build-mwan-opnsense \
        tidy fix clean

proto:
	@mkdir -p gen/mwan/v1
	$(BUF) generate

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

# build-linux and build-mwan-opnsense depend on `check` so the shared lint
# chain passes before any deploy artifact lands in $(DIST_DIR)/.
build-linux: check
	$(call build_platform,$(BINARY)-linux,linux,amd64,$(CMD))

build-mwan-opnsense: check
	$(call build_platform,mwan-opnsense,freebsd,amd64,$(CMD))

tidy:
	go mod tidy

# Run golangci-lint with --fix using the central config.
fix:
	$(GOLANGCI_LINT) run --fix $(GOLANGCI_LINT_FLAGS) $(GOLANGCI_LINT_TARGETS)

clean: clean-dist
