VERSION := $(shell tr -d '\r\n' < ../VERSION)
BINARY ?= ../0_Release/dev/hzk-doc

.PHONY: frontend build test check

frontend:
	npm --prefix web ci
	npm --prefix web run build

build: frontend
	mkdir -p "$(dir $(BINARY))"
	go build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o "$(BINARY)" ./cmd/hzk-doc

test: frontend
	go test ./...
	npm --prefix web test

check: frontend
	go vet ./...
