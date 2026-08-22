# Lint is centralized in go-makefile. Do NOT define project-local lint,
# deadcode, audit, fmt, vet, or staticcheck targets here. They duplicate
# the central pipeline and let agents bypass strict rules. Run `make help`
# for the canonical entry points (build/check/lint/fmt) and per-linter
# sub-targets (lint-golangci, lint-format, lint-gocyclo, lint-deadcode,
# staticcheck-extra). Refresh baselines via the matching *-baseline target.
#
# mwan Makefile.
# Build/lint pipeline lives in go-makefile and is fetched at runtime.
# Project-local: protobuf codegen, the YANG model gate, the cgo dependency
# recipes for the publishing binding, a docker lane for building the linux
# gateway binary on macOS, and a govulncheck wrapper that allowlists a specific
# known gobgp CVE. Shipped binaries come only from the CI release; nothing
# here cross-compiles a deploy artifact.

# Identity. Build identity is gklog's: go-makefile stamps Commit, Dirty, and
# BuildTime into goodkind.io/gklog/version for local builds and the release
# engine does the same for released archives, so a released binary reports the
# commit it was cut from and a deploy can check it after the copy. mwan's own
# internal/version reads those stamps and adds the on-disk binary hash.
BINARY     := mwan
CMD        := ./cmd/$(BINARY)
GKLOG_VPKG := goodkind.io/gklog/version

# The release engine writes dist/<name>_<goos>_<goarch>/, and the reusable
# build workflow uploads exactly that directory, so DIST_DIR is dist and the
# host build lands there too. bin/ holds only the docker lane's output.
DIST_DIR  := dist
LOCAL_BIN := bin

# Build flags applied to every binary. cgo is never pinned anywhere: native
# builds (linux CI gates, the release engine's linux job, a darwin host with
# the cgo deps provisioned) keep cgo on and compile the yangpub binding, and
# the freebsd release job builds pure Go because that package is linux-only.
GO_BUILD_EXTRA_FLAGS := -trimpath -buildvcs=false

# Pipeline modules. go-release.mk turns every shipped artifact into a signed,
# checksummed, attested GitHub release, which is what the deploy path installs
# instead of a binary built on whoever ran the playbook.
GO_MK_MODULES := go-build.mk go-release.mk

# The release set: one binary per platform, the same artifact on every host.
# The linux build links libyang and libsysrepo statically, so it can publish on
# the gateway while still running on a host that has neither, which is what the
# scp-and-run deploy contract needs. freebsd stays pure Go because the yangpub
# files are linux-tagged and the C libraries have no cross toolchain here.
#
# RELEASE_PLATFORMS is a default, not an override: each job of the release
# matrix passes its own single platform in the environment, and this must yield
# to it. Forcing both here made every job build and archive both platforms, and
# once BuildTime was stamped the two jobs' copies of one archive stopped being
# identical; the publish step's merged download then carried a freebsd archive
# no job had attested, and release 202608161945-4-9244335 failed verification.
RELEASE_PLATFORMS ?= linux/amd64 freebsd/amd64
RELEASE_BINS      := mwan:$(CMD):cgo=1,platforms=linux/amd64 mwan:$(CMD):platforms=freebsd/amd64

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

include bootstrap.mk

# The test gate runs the binary it just linked, and that binary loads libyang
# and libsysrepo at runtime. The hook installs them into a per-target prefix
# rather than a system directory, so the loader is told where they are. Only
# the pkg-config path is exported by the shared pipeline, which covers
# compiling but not running.
export LD_LIBRARY_PATH := $(GO_MK_CGO_PREFIX)/lib$(if $(strip $(LD_LIBRARY_PATH)),:$(LD_LIBRARY_PATH))

# Fully static release binary. The release engine builds with a fixed command
# whose -ldflags carries the version stamps, so these two values are the only
# way to reach the compiler and the external linker; the engine appends them
# to the inherited environment. netgo and osusergo keep name and user lookup in
# pure Go, so the binary needs no glibc name service at runtime. What the static
# archives themselves need to link (pcre2, libm) is not here: that is metadata
# of the archives, and the dependency recipe writes it into their pkg-config
# files, which cgo already reads.
#
# Both attach to the release target rather than the file, so they reach the
# release build and nothing else. A plain export puts them in the environment of
# every child process, which hands a host tool the target's link flags: building
# golangci-lint on macOS then fails with "library 'crt0.o' not found", because
# darwin has no static libc. go-makefile keeps its own cross-compiler settings
# out of the global environment for the same reason.
release: export GOFLAGS := -tags=osusergo,netgo$(if $(strip $(GOFLAGS)), $(GOFLAGS))
release: export CGO_LDFLAGS := -static

.DEFAULT_GOAL := check

# `build` is go-build.mk's: the host binary into $(DIST_DIR) behind the check
# gate. Shipped artifacts are the release engine's, and `make release` runs it
# locally; the two cross-build targets that used to feed the playbooks are gone
# because the deploy installs a published release instead.

# ---------------------------------------------------------------------------
# Project-local
# ---------------------------------------------------------------------------

# Protobuf codegen requires buf, protoc-gen-go, protoc-gen-go-grpc on PATH.
BUF    ?= buf
GOBIN  ?= $(shell go env GOPATH)/bin
export PATH := $(GOBIN):$(PATH)

GOVULNCHECK_WRAPPER := ./tools/govulncheck.sh

.PHONY: proto tidy fix clean yang-validate

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

# sysrepo compiles its repository path into the library, so this must be the
# path the gateway actually uses. Building with the build directory's prefix
# instead makes sr_connect fail on the gateway with "Operation not supported",
# which reads like a version mismatch and is not one. The install is staged
# through DESTDIR because a CI runner cannot create this directory.
WANCONFIG_SYSREPO_REPO_PATH := /etc/sysrepo

define wanconfig_cgo_dep_build
	@if [ "$$(uname -s)" = "Darwin" ]; then \
		echo "cgo dep $(1) skipped: this does not compile on macOS. Run: make build-wanconfig"; \
		exit 0; \
	fi; \
	if [ -n "$${GO_MK_TARGET_GOOS}" ] && [ "$${GO_MK_TARGET_GOOS}" != "linux" ]; then \
		echo "cgo dep $(1) skipped: no cross toolchain for $${GO_MK_TARGET_GOOS}, and only the linux artifact links it"; \
		exit 0; \
	fi; \
	if [ -f "$${GO_MK_CGO_PREFIX}/.dep-$(1)-$(2)-f$(6).stamp" ]; then \
		exit 0; \
	fi; \
	set -e; \
	rm -rf $(WANCONFIG_CGO_SRC)/$(1) $(WANCONFIG_CGO_SRC)/stage-$(1); \
	git clone --depth 1 --branch $(2) $(3) $(WANCONFIG_CGO_SRC)/$(1); \
	cmake -S $(WANCONFIG_CGO_SRC)/$(1) -B $(WANCONFIG_CGO_SRC)/$(1)/build \
		-DCMAKE_BUILD_TYPE=Release \
		-DCMAKE_INSTALL_PREFIX="$${GO_MK_CGO_PREFIX}" \
		-DCMAKE_PREFIX_PATH="$${GO_MK_CGO_PREFIX}" \
		-DCMAKE_C_FLAGS="$$(pkg-config --cflags-only-I libpcre2-8)" \
		-DBUILD_SHARED_LIBS=OFF \
		$(4) \
		-DBUILD_TESTING=OFF; \
	cmake --build $(WANCONFIG_CGO_SRC)/$(1)/build --parallel; \
	DESTDIR=$(WANCONFIG_CGO_SRC)/stage-$(1) \
		cmake --install $(WANCONFIG_CGO_SRC)/$(1)/build; \
	mkdir -p "$${GO_MK_CGO_PREFIX}"; \
	cp -R $(WANCONFIG_CGO_SRC)/stage-$(1)$${GO_MK_CGO_PREFIX}/. "$${GO_MK_CGO_PREFIX}/"; \
	if [ -n "$(strip $(5))" ]; then \
		pc="$${GO_MK_CGO_PREFIX}/lib/pkgconfig/$(1).pc"; \
		awk -v extra="$(strip $(5))" '/^Libs:/ {print $$0 " " extra; next} {print}' "$$pc" > "$$pc.tmp" \
			&& mv "$$pc.tmp" "$$pc"; \
	fi; \
	touch "$${GO_MK_CGO_PREFIX}/.dep-$(1)-$(2)-f$(6).stamp"
endef

.PHONY: go-mk-cgo-dep-libyang go-mk-cgo-dep-sysrepo

# Only the archives and headers are wanted. Each project's own executables link
# against static libyang without naming its pcre2 and libm dependencies, which
# a static archive cannot carry, so building them fails the install.
WANCONFIG_LIBYANG_CMAKE_FLAGS := -DENABLE_TOOLS=OFF

# What a consumer of the static libyang archive must link after it. The
# generated pkg-config file lists pcre2 only under Libs.private and omits libm,
# which is right for a shared library and wrong for the archive, so the recipe
# writes these onto the Libs line and cgo picks them up through its ordinary
# pkg-config lookup, in every build that links libyang: test, lint, release.
WANCONFIG_LIBYANG_STATIC_LIBS := -lpcre2-8 -lm
# SR_HAVE_DLOPEN=OFF compiles sysrepo's external-plugin machinery out of the
# library. The daemon uses no external sysrepo plugins, and the default build
# bakes the plugin directory's build-time prefix into the binary: in CI that
# is the runner's workspace under /home, and scanning it under the ifmgr
# unit's ProtectHome=true returns EACCES where a missing directory would be
# tolerated, so sr_connect failed with SR_ERR_SYS on the gateway (bisected
# live 2026-08-19). With dlopen support off there is no scan, no baked path,
# and no environment override to carry.
WANCONFIG_SYSREPO_CMAKE_FLAGS := \
	-DREPO_PATH=$(WANCONFIG_SYSREPO_REPO_PATH) \
	-DSR_HAVE_DLOPEN=OFF \
	-DENABLE_EXAMPLES=OFF \
	-DENABLE_SYSREPOCTL=OFF \
	-DENABLE_SYSREPOCFG=OFF \
	-DENABLE_SYSREPO_PLUGIND=OFF

# The build stamps and the CI cache key both carry a checksum of each
# dependency's flags, so a flag change at the same upstream version rebuilds
# everywhere instead of silently reusing the previously built archive.
WANCONFIG_LIBYANG_FLAGS_REV := $(shell printf '%s' '$(WANCONFIG_LIBYANG_CMAKE_FLAGS) $(WANCONFIG_LIBYANG_STATIC_LIBS)' | cksum | cut -d' ' -f1)
WANCONFIG_SYSREPO_FLAGS_REV := $(shell printf '%s' '$(WANCONFIG_SYSREPO_CMAKE_FLAGS)' | cksum | cut -d' ' -f1)
GO_MK_CGO_CACHE_VERSIONS := libyang=$(WANCONFIG_LIBYANG_VERSION)-f$(WANCONFIG_LIBYANG_FLAGS_REV) sysrepo=$(WANCONFIG_SYSREPO_VERSION)-f$(WANCONFIG_SYSREPO_FLAGS_REV)

go-mk-cgo-dep-libyang:
	$(call wanconfig_cgo_dep_build,libyang,$(WANCONFIG_LIBYANG_VERSION),https://github.com/CESNET/libyang.git,$(WANCONFIG_LIBYANG_CMAKE_FLAGS),$(WANCONFIG_LIBYANG_STATIC_LIBS),$(WANCONFIG_LIBYANG_FLAGS_REV))

go-mk-cgo-dep-sysrepo: go-mk-cgo-dep-libyang
	$(call wanconfig_cgo_dep_build,sysrepo,$(WANCONFIG_SYSREPO_VERSION),https://github.com/sysrepo/sysrepo.git,$(WANCONFIG_SYSREPO_CMAKE_FLAGS),,$(WANCONFIG_SYSREPO_FLAGS_REV))

# Development lane for the publishing binding on macOS, where the C libraries
# cannot be built natively. It builds the linux gateway binary inside a Debian
# trixie golang image carrying the pinned libyang/sysrepo, so the cgo binding
# links the exact library generation and glibc the gateway runs; the version
# pins come from the same group_vars home as the cgo hook. Nothing deploys its
# output: shipped binaries come from the CI release.
WANCONFIG_BUILDER_IMAGE := mwan-wanconfig-builder

.PHONY: build-wanconfig
build-wanconfig:
	@command -v docker >/dev/null 2>&1 || { \
		echo "build-wanconfig: docker is required; the binding's C libraries cannot be built on this host" >&2; \
		exit 1; }
	docker build --platform linux/amd64 \
		--build-arg LIBYANG_VERSION=$(WANCONFIG_LIBYANG_VERSION) \
		--build-arg SYSREPO_VERSION=$(WANCONFIG_SYSREPO_VERSION) \
		--build-arg "LIBYANG_CMAKE_FLAGS=$(WANCONFIG_LIBYANG_CMAKE_FLAGS)" \
		--build-arg "SYSREPO_CMAKE_FLAGS=$(WANCONFIG_SYSREPO_CMAKE_FLAGS)" \
		-t $(WANCONFIG_BUILDER_IMAGE) ../wanconfig/builder
	@mkdir -p $(LOCAL_BIN)
	docker run --rm --platform linux/amd64 \
		-v $(abspath $(CURDIR)/../..):/src -w /src/mwan/go \
		-v mwan-wanconfig-gomod:/go/pkg/mod \
		-e GOWORK=off \
		$(WANCONFIG_BUILDER_IMAGE) \
		go build $(GO_BUILD_EXTRA_FLAGS) \
		-ldflags='$(GO_BUILD_LDFLAGS)' \
		-o $(LOCAL_BIN)/$(BINARY)-wanconfig $(CMD)

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
	rm -rf $(LOCAL_BIN)
