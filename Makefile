.PHONY: help build test lint clean docker-build docker-push run-local fmt vet

# Variables
SERVICES := data-collector metrics-calculator alert-engine api-gateway
DOCKER_REGISTRY ?= ghcr.io/bl8ckfz
VERSION ?= latest

help: ## Show this help message
	@echo 'Usage: make [target]'
	@echo ''
	@echo 'Available targets:'
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'

build: ## Build all services
	@echo "Building all services..."
	@for service in $(SERVICES); do \
		echo "Building $$service..."; \
		CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o bin/$$service ./cmd/$$service; \
	done
	@echo "Build complete!"

build-%: ## Build a specific service (e.g., make build-data-collector)
	@echo "Building $*..."
	@CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o bin/$* ./cmd/$*

test: ## Run all tests
	@echo "Running tests..."
	@go test -v -race -coverprofile=coverage.txt -covermode=atomic ./...

test-integration: ## Run integration tests
	@echo "Running integration tests..."
	@go test -v -race -tags=integration ./tests/integration/...

test-e2e: ## Run end-to-end tests
	@echo "Running e2e tests..."
	@go test -v -tags=e2e ./tests/e2e/...

lint: ## Run linters
	@echo "Running linters..."
	@go vet ./...
	@gofmt -l .
	@if [ -n "$$(gofmt -l .)" ]; then \
		echo "Go files must be formatted with gofmt. Run 'make fmt'"; \
		exit 1; \
	fi

fmt: ## Format code
	@echo "Formatting code..."
	@gofmt -w .
	@go mod tidy

vet: ## Run go vet
	@go vet ./...

clean: ## Clean build artifacts
	@echo "Cleaning..."
	@rm -rf bin/
	@rm -f coverage.txt

docker-build: ## Build Docker images for all services
	@echo "Building Docker images..."
	@for service in $(SERVICES); do \
		echo "Building Docker image for $$service..."; \
		docker build -f deployments/docker/Dockerfile.$$service -t $(DOCKER_REGISTRY)/$$service:$(VERSION) .; \
	done

docker-build-%: ## Build Docker image for specific service (e.g., make docker-build-data-collector)
	@echo "Building Docker image for $*..."
	@docker build -f deployments/docker/Dockerfile.$* -t $(DOCKER_REGISTRY)/$*:$(VERSION) .

docker-push: ## Push Docker images to registry
	@echo "Pushing Docker images..."
	@for service in $(SERVICES); do \
		echo "Pushing $$service..."; \
		docker push $(DOCKER_REGISTRY)/$$service:$(VERSION); \
	done

run-local: ## Start local development environment
	@echo "Starting local development environment..."
	@docker compose up -d

stop-local: ## Stop local development environment
	@echo "Stopping local development environment..."
	@docker compose down

logs-local: ## Show logs from local environment
	@docker compose logs -f

run-%: ## Run a specific service locally (e.g., make run-data-collector)
	@echo "Running $*..."
	@go run ./cmd/$*

dev-%: ## Run a specific service with hot reload (requires air)
	@echo "Running $* with hot reload..."
	@cd cmd/$* && air

install-tools: ## Install development tools
	@echo "Installing development tools..."
	@go install github.com/air-verse/air@latest
	@echo "Tools installed!"

deps: ## Download dependencies
	@echo "Downloading dependencies..."
	@go mod download
	@go mod verify

tidy: ## Tidy dependencies
	@go mod tidy
