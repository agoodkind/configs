# Protobuf codegen requires protoc, protoc-gen-go, protoc-gen-go-grpc on PATH.
PROTOC ?= protoc
GOBIN ?= $(shell go env GOPATH)/bin
export PATH := $(GOBIN):$(PATH)

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
	go build -o bin/mwan-agent ./cmd/mwan-agent
	go build -o bin/mwan-watchdog ./cmd/mwan-watchdog

test:
	go test ./...

vet:
	go vet ./...
