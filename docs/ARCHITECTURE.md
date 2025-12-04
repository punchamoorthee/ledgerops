# LedgerOps Architecture

## Overview

LedgerOps is a double-entry accounting ledger service that prioritizes correctness over performance. The system ensures that money is never created, destroyed, or double-spent, even under high concurrency and network failures.

## System Components

### 1. API Layer (`internal/api`)

Handles HTTP requests and responses. Key responsibilities:
- Request parsing and validation (OpenAPI schema)
- Idempotency key extraction and checking
- Error handling with RFC 9457 Problem Details
- Structured logging for observability

### 2. Business Logic Layer (`internal/service`)

Contains domain logic for transfers and idempotency:

**Transfer Service:**
- Implements deterministic lock ordering to prevent deadlocks
- Validates account balances before executing transfers
- Creates transfer and ledger entry records atomically

**Idempotency Service:**
- Tracks request keys in database
- Returns cached responses for duplicate requests
- Handles concurrent requests with same key (409 Conflict)

### 3. Data Layer (`internal/store`)

PostgreSQL database with ACID guarantees:

**Tables:**
- `accounts`: User accounts with balances
- `transfers`: Transfer records (immutable)
- `ledger_entries`: Double-entry accounting records
- `idempotency_keys`: Request deduplication

**Constraints:**
- Account balance must be non-negative
- Transfer amount must be positive
- Each ledger entry is either debit or credit (not both)

**Triggers:**
- After inserting ledger entries, verify sum = 0

## Concurrency Control

### Problem: Write Skew

Without proper isolation, two concurrent transactions can both:
1. Read the same initial balance ($100)
2. Each withdraw $60
3. Final balance: $40 (should be -$20 or reject one)

Money has disappeared!

### Solution: Pessimistic Locking

We use `SELECT ... FOR UPDATE` with deterministic lock ordering:
```sql
-- Always lock in ascending order by account ID
SELECT * FROM accounts 
WHERE id IN (from_id, to_id) 
ORDER BY id ASC 
FOR UPDATE;
```

This ensures:
1. No deadlocks (circular wait impossible)
2. No write skew (concurrent reads serialized)
3. Correctness guaranteed by database

## Idempotency

### Problem: Network Failures

Client sends transfer request → timeout → retry → duplicate transfer!

### Solution: Idempotency Keys

Client generates unique key (UUID) for each logical request:
POST /transfers
Idempotency-Key: 550e8400-e29b-41d4-a716-446655440000

Server tracks key states:
- `pending`: Request in progress
- `completed`: Request succeeded, response cached
- `failed`: Request failed, error cached

Retries with same key return cached result (not new transfer).

## Data Integrity

### Database Trigger

After each ledger entry insert, trigger verifies:
```sql
SELECT SUM(debit - credit) 
FROM ledger_entries 
WHERE transfer_id = NEW.transfer_id;
```

If sum ≠ 0, transaction is rolled back.

This is our final safety net - even if application code has bugs, database rejects invalid states.

## Trade-offs

### What We Optimized For:
- ✅ Correctness (100% data integrity)
- ✅ Auditability (immutable records)
- ✅ Reliability (idempotent operations)

### What We Sacrificed:
- ❌ Raw throughput (120 TPS under high contention)
- ❌ Horizontal scalability (single-node only)
- ❌ Low latency (p99 = 450ms under contention)

For financial systems, this is the right trade-off.

## Future Enhancements

1. **Horizontal Sharding**: Partition accounts across multiple databases
2. **Read Replicas**: Scale read operations independently
3. **Event Sourcing**: Store all state changes as events
4. **Multi-Region**: Deploy across geographic regions for low latency

Each of these adds complexity - implement only when needed.