.PHONY: help build run test test-integration clean migrate-up migrate-down \
        benchmark-uniform benchmark-hotspot verify docker-build docker-up docker-down

# Variables
BINARY_NAME=ledgerops
DOCKER_IMAGE=ledgerops:latest
DB_URL=postgresql://postgres:secret@localhost:5432/ledgerops?sslmode=disable

help: ## Show this help message
	@echo 'Usage: make [target]'
	@echo ''
	@echo 'Available targets:'
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-20s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build: ## Build the server binary
	@echo "Building server..."
	@go build -o bin/server ./cmd/server
	@echo "Building benchmark tool..."
	@go build -o bin/benchmark ./cmd/benchmark
	@echo "Build complete: bin/server, bin/benchmark"

run: ## Run the server locally
	@echo "Starting server on :8080..."
	@go run ./cmd/server

test: ## Run unit tests
	@echo "Running unit tests..."
	@go test -v -race -coverprofile=coverage.out ./...

test-integration: ## Run integration tests (requires running PostgreSQL)
	@echo "Running integration tests..."
	@DB_URL=$(DB_URL) go test -v -tags=integration ./...

test-coverage: test ## Generate test coverage report
	@echo "Generating coverage report..."
	@go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

clean: ## Clean build artifacts
	@echo "Cleaning..."
	@rm -rf bin/
	@rm -f coverage.out coverage.html
	@echo "Clean complete"

migrate-up: ## Run database migrations (up)
	@echo "Running migrations..."
	@migrate -path db/migrations -database "$(DB_URL)" up
	@echo "Migrations complete"

migrate-down: ## Rollback last migration
	@echo "Rolling back last migration..."
	@migrate -path db/migrations -database "$(DB_URL)" down 1

migrate-force: ## Force migration version (use: make migrate-force V=2)
	@migrate -path db/migrations -database "$(DB_URL)" force $(V)

sqlc-generate: ## Generate Go code from SQL queries
	@echo "Generating sqlc code..."
	@sqlc generate
	@echo "Code generation complete"

benchmark-uniform: build ## Run uniform workload benchmark
	@echo "Running uniform workload benchmark..."
	@./bin/benchmark -workload=uniform -duration=60s -workers=10
	@echo "Benchmark complete"

benchmark-hotspot: build ## Run hot-spot workload benchmark
	@echo "Running hot-spot workload benchmark..."
	@./bin/benchmark -workload=hotspot -duration=60s -workers=10
	@echo "Benchmark complete"

verify: ## Verify database integrity (sum of all entries = 0)
	@echo "Verifying ledger integrity..."
	@psql $(DB_URL) -c "SELECT SUM(debit - credit) AS total_sum FROM ledger_entries;" | grep "0"
	@echo "✓ Integrity verified: sum = 0"

docker-build: ## Build Docker image
	@echo "Building Docker image..."
	@docker build -t $(DOCKER_IMAGE) .
	@echo "Image built: $(DOCKER_IMAGE)"

docker-up: ## Start services with Docker Compose
	@echo "Starting services..."
	@docker-compose up -d
	@echo "Services started. Waiting for database..."
	@sleep 5
	@make migrate-up
	@echo "Ready: http://localhost:8080"

docker-down: ## Stop services
	@echo "Stopping services..."
	@docker-compose down
	@echo "Services stopped"

docker-logs: ## View service logs
	@docker-compose logs -f

setup: ## First-time setup (install tools)
	@echo "Installing development tools..."
	@go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
	@go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest
	@echo "Tools installed. Run 'make docker-up' to start."

.DEFAULT_GOAL := help