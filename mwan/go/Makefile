# mwan Makefile.
#
# The build and lint pipeline is go-makefile, fetched at parse time by
# bootstrap.mk. Do not define lint, deadcode, audit, fmt, vet, or
# staticcheck targets here; `make help` lists the canonical entry points.
#
# Project-local targets: protobuf codegen, the YANG model gate, the cgo
# dependency recipes for the publishing binding, a docker lane for building
# the linux gateway binary on macOS, the wanconfig stack packaging, and a
# govulncheck wrapper that allowlists one known gobgp CVE.
#
# Shipped binaries come only from the CI release. The one cross build here is
# the packaging tool, which runs inside a container and never ships.

# ---------------------------------------------------------------------------
# Identity
# ---------------------------------------------------------------------------

# Build identity is gklog's. go-makefile stamps Commit, Dirty, and BuildTime
# into goodkind.io/gklog/version, so a released binary reports the commit it
# was cut from and a deploy can check it after the copy.
BINARY     := mwan
CMD        := ./cmd/$(BINARY)
GKLOG_VPKG := goodkind.io/gklog/version

# The release engine writes dist/<name>_<goos>_<goarch>/ and the build
# workflow uploads exactly that directory. bin/ holds only the docker lane's
# output.
DIST_DIR  := dist
LOCAL_BIN := bin

# cgo is never pinned here. Native builds keep cgo on and compile the yangpub
# binding; the freebsd release job builds pure Go because that package is
# linux-only.
GO_BUILD_EXTRA_FLAGS := -trimpath -buildvcs=false

# go-release.mk turns every shipped artifact into a signed, checksummed,
# attested GitHub release, which is what the deploy path installs.
GO_MK_MODULES := go-build.mk go-release.mk

# One binary per platform. The linux build links libyang and libsysrepo
# statically; freebsd stays pure Go.
#
# RELEASE_PLATFORMS is a default, not an override. Each release job passes
# its own single platform in the environment, and this must yield to it.
# Forcing both here made every job archive both platforms, and release
# 202608161945-4-9244335 then failed verification on an unattested archive.
RELEASE_PLATFORMS ?= linux/amd64 freebsd/amd64
RELEASE_BINS      := mwan:$(CMD):cgo=1,platforms=linux/amd64 mwan:$(CMD):platforms=freebsd/amd64

# gobgp's only cgo symbol is a memory-reporting helper used solely by its
# tests.
export GO_MK_CGO_OPTIONAL := github.com/osrg/gobgp/v4/internal/pkg/table

# Every lint gate runs once per shipped platform. mwan is almost entirely
# linux-tagged with a freebsd serial path, so a single host pass would skip
# most of the package.
GO_MK_PLATFORMS := linux/amd64 freebsd/amd64

# ---------------------------------------------------------------------------
# Wanconfig pins
# ---------------------------------------------------------------------------

# The six stack component versions have one home, the ansible group_vars. The
# cgo hook builds libyang and sysrepo at these pins; the stack packaging
# builds all six. A missing pin fails at parse.
WANCONFIG_PINS_FILE := ../../ansible/inventory/group_vars/all/vars.yml
wanconfig_pin = $(strip $(shell awk -F'"' '/^$(1):/ {print $$2}' $(WANCONFIG_PINS_FILE)))

WANCONFIG_LIBYANG_VERSION      := $(call wanconfig_pin,wanconfig_libyang_version)
WANCONFIG_SYSREPO_VERSION      := $(call wanconfig_pin,wanconfig_sysrepo_version)
WANCONFIG_LIBYANG_CPP_VERSION  := $(call wanconfig_pin,wanconfig_libyang_cpp_version)
WANCONFIG_SYSREPO_CPP_VERSION  := $(call wanconfig_pin,wanconfig_sysrepo_cpp_version)
WANCONFIG_NGHTTP2_ASIO_VERSION := $(call wanconfig_pin,wanconfig_nghttp2_asio_version)
WANCONFIG_ROUSETTE_VERSION     := $(call wanconfig_pin,wanconfig_rousette_version)

WANCONFIG_PINS := \
	$(WANCONFIG_LIBYANG_VERSION) \
	$(WANCONFIG_SYSREPO_VERSION) \
	$(WANCONFIG_LIBYANG_CPP_VERSION) \
	$(WANCONFIG_SYSREPO_CPP_VERSION) \
	$(WANCONFIG_NGHTTP2_ASIO_VERSION) \
	$(WANCONFIG_ROUSETTE_VERSION)

ifneq ($(words $(WANCONFIG_PINS)),6)
$(error wanconfig pins missing in $(WANCONFIG_PINS_FILE): got "$(WANCONFIG_PINS)")
endif

# go-makefile's GO_MK_CGO_DEPS hook builds libyang and sysrepo into the
# per-os/arch prefix before every gate, so the cgo files stay fully checked
# on linux CI.
GO_MK_CGO_DEPS := libyang sysrepo

include bootstrap.mk

# The test gate runs the binary it just linked, which loads libyang and
# libsysrepo at runtime from the per-target prefix.
export LD_LIBRARY_PATH := $(GO_MK_CGO_PREFIX)/lib$(if $(strip $(LD_LIBRARY_PATH)),:$(LD_LIBRARY_PATH))

# Fully static release binary. These two values are the only way to reach
# the compiler and the external linker from the release engine's fixed
# build command. They attach to the release target only: a plain export
# would hand a host tool the target's link flags, and building
# golangci-lint on macOS then fails on a missing static libc.
release: export GOFLAGS := -tags=osusergo,netgo$(if $(strip $(GOFLAGS)), $(GOFLAGS))
release: export CGO_LDFLAGS := -static

.DEFAULT_GOAL := check

# ---------------------------------------------------------------------------
# Protobuf
# ---------------------------------------------------------------------------

# Requires buf, protoc-gen-go, protoc-gen-go-grpc on PATH.
BUF   ?= buf
GOBIN ?= $(shell go env GOPATH)/bin
export PATH := $(GOBIN):$(PATH)

.PHONY: proto
proto:
	@mkdir -p gen/mwan/v1
	$(BUF) generate

# ---------------------------------------------------------------------------
# YANG model gate
# ---------------------------------------------------------------------------

# yanglint validates the modules the gateway serves itself with. Install it
# with `brew install libyang` locally; CI uses the libyang2-tools package.
# The IETF modules come from the third_party/yang submodule as an explicit
# file list at exact revisions, because the submodule also carries newer
# revisions that yanglint would otherwise prefer.
YANG_DIR     ?= ../yang
YANG_RFC_DIR ?= ../../third_party/yang/standard/ietf/RFC
YANGLINT     ?= yanglint

YANG_IETF_MODULES := \
	$(YANG_RFC_DIR)/ietf-yang-types@2025-12-22.yang \
	$(YANG_RFC_DIR)/ietf-inet-types@2025-12-22.yang \
	$(YANG_RFC_DIR)/ietf-interfaces@2018-02-20.yang \
	$(YANG_RFC_DIR)/ietf-ip@2018-02-22.yang \
	$(YANG_RFC_DIR)/ietf-routing@2018-03-13.yang \
	$(YANG_RFC_DIR)/ietf-nat@2019-01-10.yang

.PHONY: yang-validate
yang-validate:
	$(YANGLINT) --version
	$(YANGLINT) $(YANG_IETF_MODULES) $(YANG_DIR)/*.yang

check: yang-validate

# ---------------------------------------------------------------------------
# cgo dependencies for the publishing binding
# ---------------------------------------------------------------------------

# Each recipe clones its pinned tag, builds a static archive, installs it
# into the per-target prefix go.mk supplies, and stamps the version so a
# warm tree skips the rebuild.
#
# The sources live outside this Go module on purpose. A checkout inside the
# module makes the lint gates treat the tree as nested working trees, which
# turns a package absent on one platform into a hard error.
WANCONFIG_CGO_SRC := $(abspath $(CURDIR)/../../.make/wanconfig-cgo-src)

# sysrepo compiles its repository path into the library, so this must be
# the path the gateway uses. A build-directory prefix makes sr_connect fail
# on the gateway with "Operation not supported".
WANCONFIG_SYSREPO_REPO_PATH := /etc/sysrepo

# Only the archives and headers are wanted. libyang's own executables link
# against the static archive without naming pcre2 and libm, so building
# them fails the install.
WANCONFIG_LIBYANG_CMAKE_FLAGS := -DENABLE_TOOLS=OFF

# The static libyang archive needs pcre2 and libm after it. Its generated
# pkg-config file omits them, so the recipe writes them onto the Libs line
# and cgo picks them up through its ordinary pkg-config lookup.
WANCONFIG_LIBYANG_STATIC_LIBS := -lpcre2-8 -lm

# SR_HAVE_DLOPEN=OFF compiles sysrepo's external-plugin machinery out. The
# default build bakes the plugin directory's build-time prefix into the
# binary, and scanning it under the ifmgr unit's ProtectHome=true returned
# EACCES, so sr_connect failed with SR_ERR_SYS on the gateway.
WANCONFIG_SYSREPO_CMAKE_FLAGS := \
	-DREPO_PATH=$(WANCONFIG_SYSREPO_REPO_PATH) \
	-DSR_HAVE_DLOPEN=OFF \
	-DENABLE_EXAMPLES=OFF \
	-DENABLE_SYSREPOCTL=OFF \
	-DENABLE_SYSREPOCFG=OFF \
	-DENABLE_SYSREPO_PLUGIND=OFF

# The stamps and the CI cache key carry a checksum of each dependency's
# flags, so a flag change at the same version rebuilds instead of reusing a
# stale archive.
WANCONFIG_LIBYANG_FLAGS_REV := $(shell printf '%s' '$(WANCONFIG_LIBYANG_CMAKE_FLAGS) $(WANCONFIG_LIBYANG_STATIC_LIBS)' | cksum | cut -d' ' -f1)
WANCONFIG_SYSREPO_FLAGS_REV := $(shell printf '%s' '$(WANCONFIG_SYSREPO_CMAKE_FLAGS)' | cksum | cut -d' ' -f1)

WANCONFIG_LIBYANG_STAMP := $(GO_MK_CGO_PREFIX)/.dep-libyang-$(WANCONFIG_LIBYANG_VERSION)-f$(WANCONFIG_LIBYANG_FLAGS_REV).stamp
WANCONFIG_SYSREPO_STAMP := $(GO_MK_CGO_PREFIX)/.dep-sysrepo-$(WANCONFIG_SYSREPO_VERSION)-f$(WANCONFIG_SYSREPO_FLAGS_REV).stamp

# The hook is skipped on darwin, where sysrepo cannot build because it needs
# robust pthread mutexes, and for a non-linux target, which has no cross
# toolchain and never links the binding.
ifeq ($(shell uname -s),Darwin)
WANCONFIG_CGO_SKIP := sysrepo needs robust pthread mutexes, absent on darwin
endif
ifneq ($(strip $(GO_MK_TARGET_GOOS)),)
ifneq ($(GO_MK_TARGET_GOOS),linux)
WANCONFIG_CGO_SKIP := no cross toolchain for $(GO_MK_TARGET_GOOS), and only the linux artifact links it
endif
endif

# wanconfig_cgo_configure NAME FLAGS: configure a static build of a clone.
define wanconfig_cgo_configure
	cmake -S $(WANCONFIG_CGO_SRC)/$(1) -B $(WANCONFIG_CGO_SRC)/$(1)/build \
		-DCMAKE_BUILD_TYPE=Release \
		-DCMAKE_INSTALL_PREFIX=$(GO_MK_CGO_PREFIX) \
		-DCMAKE_PREFIX_PATH=$(GO_MK_CGO_PREFIX) \
		-DCMAKE_C_FLAGS="$$(pkg-config --cflags-only-I libpcre2-8)" \
		-DBUILD_SHARED_LIBS=OFF \
		$(2) \
		-DBUILD_TESTING=OFF
endef

# wanconfig_cgo_install NAME: build the configured clone and copy its DESTDIR
# stage into the prefix. The install is staged because a CI runner cannot
# create the repository path directly.
define wanconfig_cgo_install
	cmake --build $(WANCONFIG_CGO_SRC)/$(1)/build --parallel
	DESTDIR=$(WANCONFIG_CGO_SRC)/stage-$(1) cmake --install $(WANCONFIG_CGO_SRC)/$(1)/build
	mkdir -p $(GO_MK_CGO_PREFIX)
	cp -R $(WANCONFIG_CGO_SRC)/stage-$(1)$(GO_MK_CGO_PREFIX)/. $(GO_MK_CGO_PREFIX)/
endef

.PHONY: go-mk-cgo-dep-libyang go-mk-cgo-dep-sysrepo

ifdef WANCONFIG_CGO_SKIP
go-mk-cgo-dep-libyang:
	@echo "cgo dep libyang skipped: $(WANCONFIG_CGO_SKIP)"

go-mk-cgo-dep-sysrepo:
	@echo "cgo dep sysrepo skipped: $(WANCONFIG_CGO_SKIP)"
else
go-mk-cgo-dep-libyang: $(WANCONFIG_LIBYANG_STAMP)
go-mk-cgo-dep-sysrepo: $(WANCONFIG_SYSREPO_STAMP)

$(WANCONFIG_LIBYANG_STAMP):
	rm -rf $(WANCONFIG_CGO_SRC)/libyang $(WANCONFIG_CGO_SRC)/stage-libyang
	git clone --depth 1 --branch $(WANCONFIG_LIBYANG_VERSION) https://github.com/CESNET/libyang.git $(WANCONFIG_CGO_SRC)/libyang
	$(call wanconfig_cgo_configure,libyang,$(WANCONFIG_LIBYANG_CMAKE_FLAGS))
	$(call wanconfig_cgo_install,libyang)
	awk -v extra="$(WANCONFIG_LIBYANG_STATIC_LIBS)" '/^Libs:/ {print $$0 " " extra; next} {print}' $(GO_MK_CGO_PREFIX)/lib/pkgconfig/libyang.pc > $(GO_MK_CGO_PREFIX)/lib/pkgconfig/libyang.pc.tmp
	mv $(GO_MK_CGO_PREFIX)/lib/pkgconfig/libyang.pc.tmp $(GO_MK_CGO_PREFIX)/lib/pkgconfig/libyang.pc
	touch $@

$(WANCONFIG_SYSREPO_STAMP): $(WANCONFIG_LIBYANG_STAMP)
	rm -rf $(WANCONFIG_CGO_SRC)/sysrepo $(WANCONFIG_CGO_SRC)/stage-sysrepo
	git clone --depth 1 --branch $(WANCONFIG_SYSREPO_VERSION) https://github.com/sysrepo/sysrepo.git $(WANCONFIG_CGO_SRC)/sysrepo
	$(call wanconfig_cgo_configure,sysrepo,$(WANCONFIG_SYSREPO_CMAKE_FLAGS))
	$(call wanconfig_cgo_install,sysrepo)
	touch $@
endif

# ---------------------------------------------------------------------------
# Docker lane for the linux gateway binary on macOS
# ---------------------------------------------------------------------------

# Builds the linux gateway binary inside a Debian trixie golang image that
# carries the pinned libyang and sysrepo, so the cgo binding links the
# gateway's library generation and glibc. Nothing deploys its output.
WANCONFIG_BUILDER_IMAGE := mwan-wanconfig-builder

.PHONY: build-wanconfig
build-wanconfig:
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
		go build $(GO_BUILD_EXTRA_FLAGS) -ldflags='$(GO_BUILD_LDFLAGS)' -o $(LOCAL_BIN)/$(BINARY)-wanconfig $(CMD)

# ---------------------------------------------------------------------------
# Wanconfig management stack packages (MWAN-431)
# ---------------------------------------------------------------------------

# The six stack components ship as Debian packages built here and published
# with the mwan release, so a gateway installs them with apt and never
# compiles. The packaging tool is pure Go (tools/wanconfigstack). It is
# cross-built for linux and run inside a stock Debian trixie container, the
# only place the stack is ever compiled, so the packages carry trixie's
# shared-library ABI.
#
# The bundle lands in dist as wanconfig-stack_linux_<arch>.tar.gz. The
# release engine publishes, checksums, attests, and verifies every
# dist/*.tar.gz, so it rides the release unchanged.
#
# The built bundle is kept under the cgo prefix, which CI caches under a key
# that carries the stack pins, the tool sources, and the module files that
# pin the tool's dependencies. An unchanged stack is copied, not rebuilt.
#
# The architecture is amd64 unless the release engine names another: the
# gateway is amd64, and the upstream sysrepo deb template hardcodes amd64
# multiarch paths, so the local lane on an arm64 host runs the container
# under emulation rather than producing packages no gateway installs.
WANCONFIG_STACK_ARCH    ?= $(if $(strip $(GO_MK_TARGET_GOARCH)),$(GO_MK_TARGET_GOARCH),amd64)
WANCONFIG_STACK_IMAGE   := debian:trixie
WANCONFIG_STACK_SOURCES := $(wildcard tools/wanconfigstack/*.go) go.mod go.sum
WANCONFIG_STACK_BUNDLE  := wanconfig-stack_linux_$(WANCONFIG_STACK_ARCH).tar.gz
WANCONFIG_STACK_CACHE   := $(GO_MK_CGO_PREFIX)/wanconfig-stack
WANCONFIG_STACK_KEY     := $(shell printf '%s' '$(WANCONFIG_PINS) $(WANCONFIG_STACK_IMAGE)' | cat - $(WANCONFIG_STACK_SOURCES) | cksum | cut -d' ' -f1)
WANCONFIG_STACK_TOOL    := $(WANCONFIG_STACK_CACHE)/wanconfigstack-$(WANCONFIG_STACK_ARCH)
WANCONFIG_STACK_CACHED  := $(WANCONFIG_STACK_CACHE)/$(WANCONFIG_STACK_BUNDLE).$(WANCONFIG_STACK_KEY)

WANCONFIG_STACK_FLAGS := \
	-wanconfig_libyang_version $(WANCONFIG_LIBYANG_VERSION) \
	-wanconfig_sysrepo_version $(WANCONFIG_SYSREPO_VERSION) \
	-wanconfig_libyang_cpp_version $(WANCONFIG_LIBYANG_CPP_VERSION) \
	-wanconfig_sysrepo_cpp_version $(WANCONFIG_SYSREPO_CPP_VERSION) \
	-wanconfig_nghttp2_asio_version $(WANCONFIG_NGHTTP2_ASIO_VERSION) \
	-wanconfig_rousette_version $(WANCONFIG_ROUSETTE_VERSION)

GO_MK_CGO_CACHE_VERSIONS := \
	libyang=$(WANCONFIG_LIBYANG_VERSION)-f$(WANCONFIG_LIBYANG_FLAGS_REV) \
	sysrepo=$(WANCONFIG_SYSREPO_VERSION)-f$(WANCONFIG_SYSREPO_FLAGS_REV) \
	libyang-cpp=$(WANCONFIG_LIBYANG_CPP_VERSION) \
	sysrepo-cpp=$(WANCONFIG_SYSREPO_CPP_VERSION) \
	nghttp2-asio=$(WANCONFIG_NGHTTP2_ASIO_VERSION) \
	rousette=$(WANCONFIG_ROUSETTE_VERSION)
GO_MK_CGO_CACHE_INPUTS := $(WANCONFIG_STACK_SOURCES)

$(WANCONFIG_STACK_TOOL): $(WANCONFIG_STACK_SOURCES)
	@mkdir -p $(WANCONFIG_STACK_CACHE)
	GOOS=linux GOARCH=$(WANCONFIG_STACK_ARCH) CGO_ENABLED=0 go build $(GO_BUILD_EXTRA_FLAGS) -o $@ ./tools/wanconfigstack

# The bundle is built in one stock container and then proven in a second,
# fresh one: every package installs through apt, every shared library the
# stack binaries need resolves, and the datastore tool and the RESTCONF
# server start. A bundle that fails the proof is never cached.
#
# The tool is an order-only prerequisite: the cache key already hashes the
# tool sources, and a restored cache carries an older tool than the fresh
# checkout, so a timestamp dependency would rebuild the stack on every run.
$(WANCONFIG_STACK_CACHED): | $(WANCONFIG_STACK_TOOL)
	docker run --rm --platform linux/$(WANCONFIG_STACK_ARCH) \
		-v $(WANCONFIG_STACK_TOOL):/usr/local/bin/wanconfigstack:ro \
		-v $(WANCONFIG_STACK_CACHE):/out \
		$(WANCONFIG_STACK_IMAGE) \
		wanconfigstack $(WANCONFIG_STACK_FLAGS) -out /out
	docker run --rm --platform linux/$(WANCONFIG_STACK_ARCH) \
		-v $(WANCONFIG_STACK_TOOL):/usr/local/bin/wanconfigstack:ro \
		-v $(WANCONFIG_STACK_CACHE):/out:ro \
		$(WANCONFIG_STACK_IMAGE) \
		wanconfigstack -verify /out/$(WANCONFIG_STACK_BUNDLE)
	mv $(WANCONFIG_STACK_CACHE)/$(WANCONFIG_STACK_BUNDLE) $@

.PHONY: wanconfig-stack-bundle
wanconfig-stack-bundle: $(WANCONFIG_STACK_CACHED)
	@mkdir -p $(DIST_DIR)
	cp $< $(DIST_DIR)/$(WANCONFIG_STACK_BUNDLE)

# Re-run the proof against the cached bundle on demand.
.PHONY: wanconfig-stack-verify
wanconfig-stack-verify: $(WANCONFIG_STACK_TOOL)
	docker run --rm --platform linux/$(WANCONFIG_STACK_ARCH) \
		-v $(WANCONFIG_STACK_TOOL):/usr/local/bin/wanconfigstack:ro \
		-v $(WANCONFIG_STACK_CACHE):/out:ro \
		$(WANCONFIG_STACK_IMAGE) \
		wanconfigstack -verify /out/$(notdir $(WANCONFIG_STACK_CACHED))

# The release's compile stage for the linux artifact also produces the stack
# bundle, so one release tag delivers the binary and the packages it pairs
# with. The other stages and platforms never build it.
ifeq ($(RELEASE_STAGE),compile)
ifneq ($(filter linux/%,$(RELEASE_PLATFORMS)),)
release: | wanconfig-stack-bundle
endif
endif

# ---------------------------------------------------------------------------
# Maintenance
# ---------------------------------------------------------------------------

.PHONY: tidy govulncheck clean

tidy:
	go mod tidy

# mwan's wrapper filters one known gobgp vulnerability (GO-2026-4736) that
# upstream fixed in v4.3.0.
govulncheck:
	./tools/govulncheck.sh

clean: clean-dist
	rm -rf $(LOCAL_BIN)
