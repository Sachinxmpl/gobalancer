.PHONY: all build test test-cover race vet fmt fmt-check lint tidy run check clean ci help devcert

BINARY := bin/gobalancer
PKG := ./...
CONFIG ?= config.example.yaml

all: build

build:
	go build -o $(BINARY) ./cmd/gobalancer

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

tidy:
	go mod tidy

check: build
	./$(BINARY) check -c $(CONFIG)

run: build
	./$(BINARY) run -c $(CONFIG)

ci: fmt-check vet test

clean:
	rm -rf bin coverage.out

devcert:
	@mkdir -p deploy
	openssl req -x509 -newkey rsa:2048 -nodes \
		-keyout deploy/dev.key -out deploy/dev.crt \
		-days 365 -subj "/CN=localhost" \
		-addext "subjectAltName=DNS:localhost,IP:127.0.0.1"
	@echo "dev cert written to deploy/dev.crt and deploy/dev.key"

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