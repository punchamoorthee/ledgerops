# LedgerOps: Reliable Double-Entry Accounting Ledger

A production-ready, ACID-compliant ledger service that prioritizes financial correctness over raw throughput. Built with Go and PostgreSQL, LedgerOps implements deterministic concurrency control and application-layer idempotency to prevent lost updates and double-spending in distributed environments.

## Key Features

- **Serializable Isolation**: Uses PostgreSQL Serializable Snapshot Isolation (SSI) to prevent write skew and lost updates
- **Deadlock-Free**: Deterministic lock ordering mathematically precludes deadlocks during concurrent transfers
- **Exactly-Once Semantics**: Application-layer idempotency ensures duplicate requests don't create duplicate transactions
- **Database-Level Invariants**: SQL triggers enforce money conservation (sum of all debits equals sum of all credits)
- **High-Contention Testing**: Validated under hot-spot workloads simulating real-world scenarios

## Architecture

┌─────────────┐
│   Client    │
└──────┬──────┘
       │ HTTP/JSON
┌──────▼──────────────────────────┐
│      API Layer (Go)             │
│  - HTTP Handlers                │
│  - Request Validation           │
│  - Idempotency Checking         │
└──────┬──────────────────────────┘
       │
┌──────▼──────────────────────────┐
│   Business Logic                │
│  - Deterministic Lock Ordering  │
│  - Transfer Execution           │
│  - Balance Validation           │
└──────┬──────────────────────────┘
       │
┌──────▼──────────────────────────┐
│   PostgreSQL Database           │
│  - Serializable Isolation       │
│  - ACID Guarantees              │
│  - Trigger-Based Invariants     │
└─────────────────────────────────┘

## Quick Start

### Prerequisites

- Go 1.21 or higher
- PostgreSQL 15 or higher
- Docker and Docker Compose (optional, for easy setup)

### Using Docker Compose (Recommended)
```bash
# Clone the repository
git clone https://github.com/punchamoorthee/ledgerops.git
cd ledgerops

# Start the database and application
docker-compose up -d

# Run database migrations
make migrate-up

# The API is now running at http://localhost:8080
```

### Manual Setup
```bash
# Install dependencies
go mod download

# Start PostgreSQL (or use existing instance)
# Configure connection in .env file

# Run migrations
make migrate-up

# Build and run
make build
./bin/server
```

## API Examples

### Create an Account
```bash
curl -X POST http://localhost:8080/api/v1/accounts \
  -H "Content-Type: application/json" \
  -d '{"initial_balance": 100000}'
```

Response:
```json
{
  "id": 1,
  "balance": 100000,
  "created_at": "2024-12-01T10:00:00Z"
}
```

### Create a Transfer
```bash
curl -X POST http://localhost:8080/api/v1/transfers \
  -H "Content-Type: application/json" \
  -d '{
    "from_account_id": 1,
    "to_account_id": 2,
    "amount": 5000
  }'
```

### Idempotent Transfer (Safe to Retry)
```bash
curl -X POST http://localhost:8080/api/v1/transfers \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: 550e8400-e29b-41d4-a716-446655440000" \
  -d '{
    "from_account_id": 1,
    "to_account_id": 2,
    "amount": 5000
  }'
```

If you retry with the same `Idempotency-Key`, you'll get the cached response (200 OK) instead of creating a duplicate transfer.

## Running Benchmarks
```bash
# Uniform workload (low contention)
make benchmark-uniform

# Hot-spot workload (high contention)
make benchmark-hotspot

# Verify data integrity after benchmarks
make verify
```

## Project Structure

- `cmd/`: Application entry points
  - `server/`: Main HTTP server
  - `benchmark/`: Benchmarking tool
- `internal/`: Private application code
  - `api/`: HTTP handlers and routing
  - `service/`: Business logic
  - `store/`: Database layer
- `db/`: Database migrations and queries
- `api/`: OpenAPI specification
- `docs/`: Documentation

## Design Decisions

### Why Pessimistic Locking?

We use `SELECT ... FOR UPDATE` with deterministic lock ordering instead of optimistic concurrency control. While this has higher latency under contention, it provides absolute correctness guarantees - critical for financial systems where data loss is unacceptable.

### Why Application-Layer Idempotency?

Network layers can't guarantee exactly-once delivery. By implementing idempotency at the application layer with unique request keys, we ensure that retries due to timeouts don't create duplicate transactions.

### Why Database Triggers?

The database trigger that validates `SUM(debit - credit) = 0` acts as a final safety net. Even if application logic has bugs, the database will reject any transaction that violates money conservation.

## Performance Characteristics

**Uniform Workload (Low Contention):**
- Throughput: ~850 TPS
- p50 Latency: ~12ms
- p99 Latency: ~45ms
- Abort Rate: <3%

**Hot-Spot Workload (High Contention):**
- Throughput: ~120 TPS
- p50 Latency: ~35ms
- p99 Latency: ~450ms
- Abort Rate: ~87%

The high abort rate under contention is expected and correct - it demonstrates that the system properly serializes conflicting transactions, prioritizing safety over throughput.

## Testing
```bash
# Run unit tests
make test

# Run integration tests (requires running PostgreSQL)
make test-integration

# Run all tests with coverage
make test-coverage
```

## Documentation

- [Architecture Overview](docs/ARCHITECTURE.md)
- [Benchmark Results](docs/BENCHMARKS.md)
- [OpenAPI Specification](api/openapi.yaml)

## Related Work

This project was developed as part of my Master's thesis at California State University, Dominguez Hills. It applies first-principles thinking to the problem of maintaining financial correctness in concurrent, distributed systems.

**Key Papers Referenced:**
- Gray & Reuter (1993) - Transaction Processing: Concepts and Techniques
- Berenson et al. (1995) - A Critique of ANSI SQL Isolation Levels
- Ports & Grittner (2012) - Serializable Snapshot Isolation in PostgreSQL

## License

MIT License - see [LICENSE](LICENSE) file for details.

## Author

Nanu Panchamurthy  
Master of Science in Computer Science  
California State University, Dominguez Hills  
[punchamoorthee@gmail.com](mailto:punchamoorthee@gmail.com)

## Acknowledgments

Thanks to the PostgreSQL team for their excellent implementation of Serializable Snapshot Isolation, and to the Go community for building such a productive ecosystem.