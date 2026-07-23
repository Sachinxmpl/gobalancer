.PHONY: all build test test-cover race vet fmt fmt-check lint run check clean ci help

BINARY := bin/gobalancer
PKG := ./...
CONFIG ?= config.example.yaml

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
BUILD_DATE := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -X main.version=$(VERSION) \
           -X main.commit=$(COMMIT) \
           -X main.buildDate=$(BUILD_DATE)

all: build

build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/gobalancer

test:
	go test -race -count=1 $(PKG)

test-cover:
	go test -race -count=1 -coverprofile=coverage.out -covermode=atomic $(PKG)
	go tool cover -func=coverage.out

vet:
	go vet $(PKG)

fmt:
	gofmt -w ./cmd ./internal

fmt-check:
	@files=$$(gofmt -l ./cmd ./internal); \
	if [ -n "$$files" ]; then \
		echo "The following files need formatting:"; \
		echo "$$files"; \
		exit 1; \
	fi

lint:
	golangci-lint run

check: build
	./$(BINARY) check -c $(CONFIG)

run: build
	./$(BINARY) run -c $(CONFIG)

ci: fmt-check vet test

clean:
	rm -rf bin coverage.out

help:
	@echo "Available targets:"
	@echo "  build        Build the binary"
	@echo "  test         Run tests with race detector"
	@echo "  test-cover   Run tests with coverage"
	@echo "  vet          Run go vet"
	@echo "  fmt          Format Go files"
	@echo "  fmt-check    Verify formatting"
	@echo "  lint         Run golangci-lint"
	@echo "  tidy         Run go mod tidy"
	@echo "  check        Validate a config file"
	@echo "  run          Build and run the server"
	@echo "  ci           Run CI checks"
	@echo "  clean        Remove build artifacts"