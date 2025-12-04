.PHONY: build run seed benchmark clean docker-up docker-down

# Binary Names
API_BIN=bin/api
BENCH_BIN=bin/benchmark
SEED_BIN=bin/seeder

# Database URL for local tools (Seeder/Migrate)
DB_URL=postgresql://admin:secret@localhost:5433/ledger?sslmode=disable

build:
	@echo "Building binaries..."
	@go build -o $(API_BIN) ./cmd/api
	@go build -o $(BENCH_BIN) ./cmd/benchmark
	@go build -o $(SEED_BIN) ./cmd/seeder

docker-up:
	@docker-compose up -d --build
	@echo "Waiting for DB..."
	@sleep 5
	@make migrate-up

docker-down:
	@docker-compose down -v

migrate-up:
	@migrate -path db/migrations -database "$(DB_URL)" up

migrate-down:
	@migrate -path db/migrations -database "$(DB_URL)" down -all

seed: build
	@DB_SOURCE="$(DB_URL)" ./$(SEED_BIN)

benchmark-uniform: build
	@echo "Running Uniform Workload..."
	@./$(BENCH_BIN) -workload=uniform -workers=10 -duration=30s -url=http://localhost:8080

benchmark-hotspot: build
	@echo "Running Hotspot Workload..."
	@./$(BENCH_BIN) -workload=hotspot -workers=50 -duration=30s -url=http://localhost:8080

plot:
	@python3 analysis/generate_plots.py

full-cycle: docker-down docker-up seed benchmark-uniform benchmark-hotspot plot