.PHONY: build dev test clean frontend backend all

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

# Docker build
docker:
	docker build -t indelible:$(VERSION) .
