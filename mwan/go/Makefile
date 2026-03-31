# Protobuf codegen requires protoc, protoc-gen-go, protoc-gen-go-grpc on PATH.
PROTOC ?= protoc
GOBIN ?= $(shell go env GOPATH)/bin
export PATH := $(GOBIN):$(PATH)

GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
GIT_DIRTY := $(shell git diff --quiet HEAD -- . 2>/dev/null && echo clean || echo dirty)
LDFLAGS := -X main.gitCommit=$(GIT_COMMIT) -X main.gitDirty=$(GIT_DIRTY)

.PHONY: all proto build test vet

all: build

proto:
	@mkdir -p gen/mwan/v1
	$(PROTOC) -I proto \
		--go_out=gen --go_opt=paths=source_relative \
		--go-grpc_out=gen --go-grpc_opt=paths=source_relative \
		mwan/v1/mwan.proto

build: proto
	@mkdir -p bin
	go build -trimpath -ldflags="$(LDFLAGS)" -o bin/mwan ./cmd/mwan

test:
	go test ./...

vet:
	go vet ./...
