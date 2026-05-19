.PHONY: build dev test clean frontend backend all security fuzz bench check ci-local ci-dev1

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")

all: build

# Build Vue frontend
frontend:
	cd web && npm install && npm run build

# Build Go binary (requires frontend built first)
backend:
	go build -ldflags "-X github.com/WithAutonomi/indelible/internal/buildinfo.Version=$(VERSION)" -o bin/indelible ./cmd/indelible

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
	GOOS=linux GOARCH=amd64 go build -ldflags "-X github.com/WithAutonomi/indelible/internal/buildinfo.Version=$(VERSION)" -o bin/indelible-linux-amd64 ./cmd/indelible

# One-shot ops tool: republish DataMaps of pre-existing private uploads. See
# cmd/migrate-publish-datamaps/main.go. Requires antd >= 0.7.0.
migrate-publish-datamaps:
	go build -o bin/migrate-publish-datamaps ./cmd/migrate-publish-datamaps

migrate-publish-datamaps-linux:
	GOOS=linux GOARCH=amd64 go build -o bin/migrate-publish-datamaps-linux-amd64 ./cmd/migrate-publish-datamaps

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

# Run the PR-gate CI subset locally (lint, vet, mod verify, swag drift,
# frontend build/test, sqlite Go tests). Mirrors what we still run in CI on
# every PR after the May 2026 trim.
ci-local:
	bash scripts/ci-local.sh

# Run the heavyweight CI matrix (race detection, postgres tests, docker
# build/smoke, Playwright E2E) on the dev1 Linux test box via SSH. CI on
# master picks these up post-merge, but use this before pushing if you've
# touched the Dockerfile, dialect-sensitive SQL, or anything race-prone.
# Pass flags with ARGS=... e.g. `make ci-dev1 ARGS="--only e2e"`.
ci-dev1:
	bash scripts/ci-dev1.sh $(ARGS)
