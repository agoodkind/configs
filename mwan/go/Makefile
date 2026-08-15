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

# Build flags applied to every binary. cgo is never pinned anywhere: Go
# defaults CGO_ENABLED=0 for cross-compiles without a matching C toolchain,
# so the from-macOS cross artifacts stay pure-Go and static, while native
# builds (linux CI gates, a darwin host with the cgo deps provisioned) keep
# cgo on and compile the yangpub binding.
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

# Wanconfig cgo dependencies (MWAN-359). The yangpub binding links libyang and
# libsysrepo through cgo; go-makefile's GO_MK_CGO_DEPS hook builds both at the
# gateway's pinned versions into the per-os/arch prefix before every gate, so
# the cgo files stay fully checked on linux, where CI runs the gates natively.
# The pins have one home, the ansible group_vars; a missing pin fails at parse.
WANCONFIG_PINS_FILE := ../../ansible/inventory/group_vars/all/vars.yml
WANCONFIG_LIBYANG_VERSION := $(strip $(shell awk -F'"' '/^wanconfig_libyang_version:/ {print $$2}' $(WANCONFIG_PINS_FILE)))
WANCONFIG_SYSREPO_VERSION := $(strip $(shell awk -F'"' '/^wanconfig_sysrepo_version:/ {print $$2}' $(WANCONFIG_PINS_FILE)))
ifeq ($(WANCONFIG_LIBYANG_VERSION),)
$(error wanconfig_libyang_version not found in $(WANCONFIG_PINS_FILE))
endif
ifeq ($(WANCONFIG_SYSREPO_VERSION),)
$(error wanconfig_sysrepo_version not found in $(WANCONFIG_PINS_FILE))
endif
GO_MK_CGO_DEPS := libyang sysrepo
GO_MK_CGO_CACHE_VERSIONS := libyang=$(WANCONFIG_LIBYANG_VERSION) sysrepo=$(WANCONFIG_SYSREPO_VERSION)

include bootstrap.mk

# The test gate runs the binary it just linked, and that binary loads libyang
# and libsysrepo at runtime. The hook installs them into a per-target prefix
# rather than a system directory, so the loader is told where they are. Only
# the pkg-config path is exported by the shared pipeline, which covers
# compiling but not running.
export LD_LIBRARY_PATH := $(GO_MK_CGO_PREFIX)/lib$(if $(strip $(LD_LIBRARY_PATH)),:$(LD_LIBRARY_PATH))

.DEFAULT_GOAL := check

# Acceptance gate. `make build` fails unless every shipped output compiles
# cleanly, the publishing-enabled wanconfig binary included. Add new outputs
# to this prerequisite list when we start shipping them.
build: proto build-linux build-mwan-opnsense build-linux-wanconfig

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
# $(1)=output binary name, $(2)=GOOS, $(3)=GOARCH, $(4)=cmd path
define build_platform
	@mkdir -p $(DIST_DIR)
	GOOS=$(2) GOARCH=$(3) go build $(GO_BUILD_EXTRA_FLAGS) \
		-ldflags='$(GO_BUILD_LDFLAGS)' \
		-o $(DIST_DIR)/$(1) $(4)
endef

# build-linux and build-mwan-opnsense depend on `check`, which fans
# out to: build vet lint test govulncheck staticcheck-extra. Full lint
# pass is mandatory before any deploy artifact lands in $(DIST_DIR)/.
# No CGO_ENABLED pins: both are cross-compiles from every deploy host, so
# Go's toolchain default keeps them static; the publishing-enabled gateway
# binary comes from the docker builder lane below.
build-linux: check
	$(call build_platform,$(BINARY)-linux,linux,amd64,$(CMD))

build-mwan-opnsense: check
	$(call build_platform,mwan-opnsense,freebsd,amd64,$(CMD))

# Cgo dependency recipes for the go-makefile hook. Each clones its pinned
# tag and installs into the per-target prefix go.mk supplies through the
# environment, then stamps the version so a warm tree skips the rebuild.
# libyang needs pcre2 (apt libpcre2-dev, brew pcre2); sysrepo links libyang,
# so its configure searches the prefix first and its recipe orders after.
# The hook skips on darwin: sysrepo hard-requires robust pthread mutexes
# (src/common.c calls pthread_mutexattr_setrobust unconditionally), an API
# darwin has never shipped, so the libraries cannot exist on macOS and the
# yangpub files stay linux-tagged. Linux CI checks them fully; macOS builds
# the gateway binary through the docker lane.
# The sources live outside this Go module on purpose. Each clone carries its
# own .git, and a checkout inside the module makes the lint gates treat the
# tree as holding nested working trees, which changes how they name their
# targets and turns a package that is absent on one platform of the matrix
# into a hard error.
WANCONFIG_CGO_SRC := $(abspath $(CURDIR)/../../.make/wanconfig-cgo-src)

define wanconfig_cgo_dep_build
	@if [ "$$(uname -s)" = "Darwin" ]; then \
		echo "cgo dep $(1) skipped: sysrepo needs robust pthread mutexes, absent on darwin"; \
		exit 0; \
	fi; \
	if [ -f "$${GO_MK_CGO_PREFIX}/.dep-$(1)-$(2).stamp" ]; then \
		exit 0; \
	fi; \
	set -e; \
	rm -rf $(WANCONFIG_CGO_SRC)/$(1); \
	git clone --depth 1 --branch $(2) $(3) $(WANCONFIG_CGO_SRC)/$(1); \
	cmake -S $(WANCONFIG_CGO_SRC)/$(1) -B $(WANCONFIG_CGO_SRC)/$(1)/build \
		-DCMAKE_BUILD_TYPE=Release \
		-DCMAKE_INSTALL_PREFIX="$${GO_MK_CGO_PREFIX}" \
		-DCMAKE_PREFIX_PATH="$${GO_MK_CGO_PREFIX}" \
		-DCMAKE_C_FLAGS="$$(pkg-config --cflags-only-I libpcre2-8)" \
		-DREPO_PATH="$${GO_MK_CGO_PREFIX}/etc/sysrepo" \
		-DBUILD_TESTING=OFF; \
	cmake --build $(WANCONFIG_CGO_SRC)/$(1)/build --parallel; \
	cmake --install $(WANCONFIG_CGO_SRC)/$(1)/build; \
	touch "$${GO_MK_CGO_PREFIX}/.dep-$(1)-$(2).stamp"
endef

.PHONY: go-mk-cgo-dep-libyang go-mk-cgo-dep-sysrepo
go-mk-cgo-dep-libyang:
	$(call wanconfig_cgo_dep_build,libyang,$(WANCONFIG_LIBYANG_VERSION),https://github.com/CESNET/libyang.git)

go-mk-cgo-dep-sysrepo: go-mk-cgo-dep-libyang
	$(call wanconfig_cgo_dep_build,sysrepo,$(WANCONFIG_SYSREPO_VERSION),https://github.com/sysrepo/sysrepo.git)

# Publishing-enabled gateway binary (MWAN-358). Builds inside a Debian
# trixie golang image carrying the pinned libyang/sysrepo, so the cgo
# binding links the exact library generation and glibc the gateway runs;
# the version pins come from the same group_vars home as the cgo hook.
WANCONFIG_BUILDER_IMAGE := mwan-wanconfig-builder

.PHONY: build-linux-wanconfig
build-linux-wanconfig:
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
