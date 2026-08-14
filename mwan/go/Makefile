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
# monolith entrypoints, plus a govulncheck wrapper that allowlists a
# specific known gobgp CVE.

# Identity. mwan's internal/version pkg uses lowercase unexported fields
# (gitCommit, gitDirty), so VPKG canonical stamping is skipped. Pre-populate
# GO_BUILD_LDFLAGS BEFORE -include $(GO_MK); go-build.mk's ?= preserves it.
BINARY   := mwan
CMD      := ./cmd/$(BINARY)
DIST_DIR := bin

# Build flags applied to every binary. cgo is not globally disabled: Go
# itself defaults CGO_ENABLED=0 for cross-compiles when no C toolchain is
# configured, which keeps the from-macOS cross builds pure Go, and the
# wanconfig publishing binding (linux && cgo) compiles only in the docker
# builder lane. The freebsd target pins CGO_ENABLED=0 explicitly.
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

# gobgp's only cgo symbol is a memory-reporting helper used solely by its tests.
export GO_MK_CGO_OPTIONAL := github.com/osrg/gobgp/v4/internal/pkg/table

# Run every lint gate once per shipped platform, not just the host. mwan is
# almost entirely linux-tagged with a freebsd serial path, so a single host
# pass (linux on CI, darwin locally) skips most of the package. This is the one
# committed source of truth, so local and CI analyze the same targets.
GO_MK_PLATFORMS := linux/amd64 freebsd/amd64

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

GOVULNCHECK_WRAPPER := ./tools/govulncheck.sh

.PHONY: proto build-linux build-mwan-opnsense \
        tidy fix clean yang-validate

proto:
	@mkdir -p gen/mwan/v1
	$(BUF) generate

# Model gate. yanglint (ships with libyang) validates the YANG modules the
# gateway serves itself with; any parse error or unresolved reference fails
# check. yanglint installs manually per the wanconfig toolchain decision:
# `brew install libyang` locally, apt package libyang2-tools in CI.
# The IETF modules come from the third_party/yang submodule (YangModels).
# They are loaded as an explicit file list at exact revisions, because the
# submodule tree also carries newer revisions that yanglint would otherwise
# prefer for unversioned imports.
YANG_DIR ?= ../yang
YANG_RFC_DIR ?= ../../third_party/yang/standard/ietf/RFC
YANGLINT ?= yanglint
YANG_IETF_MODULES := \
	$(YANG_RFC_DIR)/ietf-yang-types@2025-12-22.yang \
	$(YANG_RFC_DIR)/ietf-inet-types@2025-12-22.yang \
	$(YANG_RFC_DIR)/ietf-interfaces@2018-02-20.yang \
	$(YANG_RFC_DIR)/ietf-ip@2018-02-22.yang \
	$(YANG_RFC_DIR)/ietf-routing@2018-03-13.yang \
	$(YANG_RFC_DIR)/ietf-nat@2019-01-10.yang

yang-validate:
	$(YANGLINT) --version
	$(YANGLINT) $(YANG_IETF_MODULES) $(YANG_DIR)/*.yang

check: yang-validate

# Reusable cross-platform build template. Pulls GO_BUILD_LDFLAGS and
# GO_BUILD_EXTRA_FLAGS from the canonical pipeline so every binary, host
# and cross-compile, lands with the same flag set.
#
# $(1)=output binary name, $(2)=GOOS, $(3)=GOARCH, $(4)=cmd path,
# $(5)=extra env (for example CGO_ENABLED=0)
define build_platform
	@mkdir -p $(DIST_DIR)
	GOOS=$(2) GOARCH=$(3) $(5) go build $(GO_BUILD_EXTRA_FLAGS) \
		-ldflags='$(GO_BUILD_LDFLAGS)' \
		-o $(DIST_DIR)/$(1) $(4)
endef

# build-linux and build-mwan-opnsense depend on `check`, which fans
# out to: build vet lint test govulncheck staticcheck-extra. Full lint
# pass is mandatory before any deploy artifact lands in $(DIST_DIR)/.
# Both deployable artifacts pin CGO_ENABLED=0: they must stay static and
# portable, and the linux one also builds natively in CI where cgo would
# otherwise default on and demand the sysrepo headers. The
# publishing-enabled gateway binary comes from the docker builder lane.
build-linux: check
	$(call build_platform,$(BINARY)-linux,linux,amd64,$(CMD),CGO_ENABLED=0)

build-mwan-opnsense: check
	$(call build_platform,mwan-opnsense,freebsd,amd64,$(CMD),CGO_ENABLED=0)

# Publishing-enabled gateway binary (MWAN-358). Builds inside a Debian
# trixie golang image carrying the pinned libyang/sysrepo, so the cgo
# binding links the library generation the gateway runs. The version pins
# are passed in by the caller; the deploy reads them from group_vars.
WANCONFIG_BUILDER_IMAGE := mwan-wanconfig-builder

.PHONY: build-linux-wanconfig
build-linux-wanconfig:
	$(if $(WANCONFIG_LIBYANG_VERSION),,$(error WANCONFIG_LIBYANG_VERSION is required))
	$(if $(WANCONFIG_SYSREPO_VERSION),,$(error WANCONFIG_SYSREPO_VERSION is required))
	docker build --platform linux/amd64 \
		--build-arg LIBYANG_VERSION=$(WANCONFIG_LIBYANG_VERSION) \
		--build-arg SYSREPO_VERSION=$(WANCONFIG_SYSREPO_VERSION) \
		-t $(WANCONFIG_BUILDER_IMAGE) ../wanconfig/builder
	@mkdir -p $(DIST_DIR)
	docker run --rm --platform linux/amd64 \
		-v $(abspath $(CURDIR)/../..):/src -w /src/mwan/go \
		-v mwan-wanconfig-gomod:/go/pkg/mod \
		-e GOWORK=off -e CGO_ENABLED=1 \
		$(WANCONFIG_BUILDER_IMAGE) \
		go build $(GO_BUILD_EXTRA_FLAGS) \
		-ldflags='$(GO_BUILD_LDFLAGS)' \
		-o $(DIST_DIR)/$(BINARY)-linux-wanconfig $(CMD)

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
