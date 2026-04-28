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

.PHONY: proto build build-linux clean tidy fmt fix staticcheck-clyde

fmt:
	golangci-lint fmt ./...

fix:
	golangci-lint run --fix ./...

# clyde-staticcheck = Clyde's custom analyzer set:
# - slog_error_without_err: error-level slog must carry err field
# - banned_direct_output:  no fmt.Print*, no stdlib log.Print/Fatal/Panic
# - hot_loop_info_log:     no Info-level slog directly inside loops
# - missing_boundary_log:  main() must emit a structured slog event
# - no_any_or_empty_interface: exported types/funcs must not use any/interface{}
#
# Runs against an upstream binary built from goodkind.io/clyde local clone.
# Falls back to a polite skip if clyde isn't checked out.
CLYDE_REPO := $(HOME)/Sites/clyde-dev/clyde
CLYDE_BIN  := .make/clyde-staticcheck
$(CLYDE_BIN): $(wildcard $(CLYDE_REPO)/internal/staticcheck/*.go) $(CLYDE_REPO)/cmd/clyde-staticcheck/main.go
	@if [ ! -d "$(CLYDE_REPO)" ]; then \
		echo "clyde-staticcheck: $(CLYDE_REPO) not present; skipping"; \
		exit 0; \
	fi
	@mkdir -p .make
	cd $(CLYDE_REPO) && go build -o $(CURDIR)/$(CLYDE_BIN) ./cmd/clyde-staticcheck

staticcheck-clyde: $(CLYDE_BIN)
	@if [ ! -x "$(CLYDE_BIN)" ]; then \
		echo "clyde-staticcheck binary not built; skipping"; \
		exit 0; \
	fi
	@bash -c 'set -o pipefail; \
		$(CLYDE_BIN) -slog_error_without_err -banned_direct_output \
			-hot_loop_info_log -missing_boundary_log \
			-no_any_or_empty_interface ./... 2>&1 \
			| sed "s|$(CURDIR)/||g" | sort > .make/staticcheck-clyde.out; \
		new=$$(comm -23 .make/staticcheck-clyde.out .staticcheck-clyde-baseline.txt || true); \
		if [ -n "$$new" ]; then \
			echo "NEW staticcheck-clyde findings (not in baseline):"; \
			echo "$$new"; \
			echo ""; \
			echo "Either fix them, or update the baseline with:"; \
			echo "  make staticcheck-clyde-baseline"; \
			echo "and document the exception in STATICCHECK-NOTES.md"; \
			exit 1; \
		fi; \
		gone=$$(comm -13 .make/staticcheck-clyde.out .staticcheck-clyde-baseline.txt || true); \
		if [ -n "$$gone" ]; then \
			echo "RESOLVED staticcheck-clyde findings (please update baseline):"; \
			echo "$$gone"; \
		fi; \
		n=$$(wc -l < .make/staticcheck-clyde.out); \
		echo "staticcheck-clyde: OK ($$n findings, all in baseline)"'

staticcheck-clyde-baseline: $(CLYDE_BIN)
	@$(CLYDE_BIN) -slog_error_without_err -banned_direct_output \
		-hot_loop_info_log -missing_boundary_log \
		-no_any_or_empty_interface ./... 2>&1 \
		| sed "s|$(CURDIR)/||g" | sort > .staticcheck-clyde-baseline.txt
	@echo "Baseline updated: $$(wc -l < .staticcheck-clyde-baseline.txt | tr -d ' ') findings"

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
