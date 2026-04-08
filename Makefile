.PHONY: build dev test clean frontend backend all security fuzz bench check

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")

all: build

# Build Vue frontend
frontend:
	cd web && npm install && npm run build

# Build Go binary (requires frontend built first)
backend:
	go build -ldflags "-X main.version=$(VERSION)" -o bin/indelible ./cmd/indelible

# Build everything
build: frontend backend
	@echo "Built bin/indelible $(VERSION)"

# Development: run Go backend with hot reload (requires air)
dev-backend:
	air -c .air.toml

# Development: run Vue dev server
dev-frontend:
	cd web && npm run dev

# Run both in parallel (requires make -j2)
dev: dev-backend dev-frontend

# Run tests
test:
	go test ./...

# Clean build artifacts
clean:
	rm -rf bin/ web/dist/ web/node_modules/

# Run database migrations only (useful for dev)
migrate:
	go run ./cmd/indelible --migrate-only

# Cross-compile for Linux
build-linux:
	cd web && npm install && npm run build
	GOOS=linux GOARCH=amd64 go build -ldflags "-X main.version=$(VERSION)" -o bin/indelible-linux-amd64 ./cmd/indelible

# Security scanning (govulncheck + npm audit)
security:
	govulncheck ./...
	cd web && npm audit --audit-level=high

# Run fuzz tests (30s each)
fuzz:
	go test -fuzz=FuzzParseSelector -fuzztime 30s ./internal/services/
	go test -fuzz=FuzzValidateToken -fuzztime 30s ./internal/auth/

# Run benchmarks
bench:
	go test -bench=. -benchmem -benchtime 3s ./internal/handlers/

# Run all quality checks (lint + test + security)
check: test
	golangci-lint run ./...
	govulncheck ./...

# Docker build
docker:
	docker build -t indelible:$(VERSION) .
