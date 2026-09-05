.PHONY: help build test lint generate migrate-up migrate-down dev-up dev-down docker-build docker-push clean doctor release-check release-snapshot

# Default target
help:
	@echo "Simple-VPN-Builder - Developer-First VPN Control Plane"
	@echo ""
	@echo "Available targets:"
	@echo "  build            Build control-plane and agent binaries"
	@echo "  test             Run all tests with race detector"
	@echo "  test-coverage    Run tests with coverage report"
	@echo "  lint             Run golangci-lint"
	@echo "  doctor           Run node agent diagnostic check"
	@echo "  generate         Generate code (sqlc, protobuf, openapi)"
	@echo "  generate-proto   Generate Go code from protobuf"
	@echo "  generate-sqlc    Generate Go code from SQL"
	@echo "  generate-openapi Generate Go types from OpenAPI spec"
	@echo "  migrate-up       Run database migrations up"
	@echo "  migrate-down     Run database migrations down"
	@echo "  migrate-create   Create new migration file"
	@echo "  dev-up           Start local development stack (docker-compose)"
	@echo "  dev-down         Stop local development stack"
	@echo "  dev-logs         Follow docker-compose logs"
	@echo "  docker-build     Build Docker images"
	@echo "  docker-push      Push Docker images"
	@echo "  release-check    Validate GoReleaser configuration"
	@echo "  release-snapshot Build snapshot release locally without publishing"
	@echo "  clean            Clean build artifacts"
	@echo "  install-tools    Install development tools"

# Variables
BINARY_DIR := ./bin
CP_BINARY := $(BINARY_DIR)/vpnbuilder-cp
AGENT_BINARY := $(BINARY_DIR)/vpnbuilder-agent
BOT_BINARY := $(BINARY_DIR)/vpnbuilder-bot
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME := $(shell date -u '+%Y-%m-%d_%H:%M:%S')
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.buildTime=$(BUILD_TIME)

# Build targets
build: $(CP_BINARY) $(AGENT_BINARY) $(BOT_BINARY)

$(CP_BINARY):
	@mkdir -p $(BINARY_DIR)
	go build -ldflags "$(LDFLAGS)" -o $(CP_BINARY) ./cmd/control-plane

$(AGENT_BINARY):
	@mkdir -p $(BINARY_DIR)
	go build -ldflags "$(LDFLAGS)" -o $(AGENT_BINARY) ./cmd/agent

$(BOT_BINARY):
	@mkdir -p $(BINARY_DIR)
	go build -ldflags "$(LDFLAGS)" -o $(BOT_BINARY) ./cmd/bot

# Test targets
test:
	go test -race -count=1 ./...

test-coverage:
	go test -race -count=1 -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

test-integration:
	go test -race -count=1 -tags=integration ./...

# Lint
lint:
	golangci-lint run ./...

# Diagnostics
doctor: $(AGENT_BINARY)
	$(AGENT_BINARY) doctor

# Code generation
generate: generate-proto generate-sqlc generate-openapi

generate-proto:
	@which buf > /dev/null || (echo "buf not installed" && exit 1)
	buf generate

generate-sqlc:
	@which sqlc > /dev/null || (echo "sqlc not installed" && exit 1)
	sqlc generate

generate-openapi:
	@which oapi-codegen > /dev/null || (echo "oapi-codegen not installed" && exit 1)
	oapi-codegen -generate types,chi-server -package openapi -o pkg/openapi/api.gen.go api/openapi.yaml

# Database migrations
migrate-up:
	@which migrate > /dev/null || (echo "migrate not installed" && exit 1)
	migrate -path migrations -database "$(DATABASE_URL)" up

migrate-down:
	@which migrate > /dev/null || (echo "migrate not installed" && exit 1)
	migrate -path migrations -database "$(DATABASE_URL)" down 1

migrate-create:
	@read -p "Migration name: " name; \
	migrate create -ext sql -dir migrations -seq $$name

# Development
dev-up:
	docker-compose -f docker/docker-compose.yml up -d

dev-down:
	docker-compose -f docker/docker-compose.yml down -v

dev-logs:
	docker-compose -f docker/docker-compose.yml logs -f

dev-restart:
	docker-compose -f docker/docker-compose.yml restart

# Docker
docker-build:
	docker build -f docker/control-plane.Dockerfile -t vpnbuilder-cp:$(VERSION) .
	docker build -f docker/agent.Dockerfile -t vpnbuilder-agent:$(VERSION) .

docker-push:
	docker tag vpnbuilder-cp:$(VERSION) ghcr.io/ivanchik-byte/simple-vpn-builder-cp:$(VERSION)
	docker tag vpnbuilder-agent:$(VERSION) ghcr.io/ivanchik-byte/simple-vpn-builder-agent:$(VERSION)
	docker push ghcr.io/ivanchik-byte/simple-vpn-builder-cp:$(VERSION)
	docker push ghcr.io/ivanchik-byte/simple-vpn-builder-agent:$(VERSION)

# Release automation
release-check:
	@which goreleaser > /dev/null || (echo "goreleaser not installed, install from https://goreleaser.com" && exit 1)
	goreleaser check

release-snapshot:
	@which goreleaser > /dev/null || (echo "goreleaser not installed, install from https://goreleaser.com" && exit 1)
	goreleaser release --snapshot --clean --skip=publish

# Clean
clean:
	rm -rf $(BINARY_DIR) coverage.out coverage.html

# Tools
install-tools:
	go install github.com/golang-migrate/migrate/v4/cmd/migrate@latest
	go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
	go install github.com/bufbuild/buf/cmd/buf@latest
	go install github.com/deepmap/oapi-codegen/cmd/oapi-codegen@latest
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	go install github.com/cosmtrek/air@latest

# Run locally (with air for hot reload)
run-cp:
	air -c .air.cp.toml

run-agent:
	air -c .air.agent.toml

# Tidy
tidy:
	go mod tidy
	go mod verify

# Verify
verify: tidy lint test