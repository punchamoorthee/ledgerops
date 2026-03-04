# LedgerOps

A distributed ledger system built to answer one question: what does it actually take to make money move correctly?

Not "correctly under normal conditions" — correctly when clients retry, when the network lies, when two transfers touch the same accounts at the same time, and when the application layer makes a mistake. The guarantees in this system live at the database level, not in application code that can be bypassed, miscalled, or partially executed.

This is my MS thesis project at CSUDH. The paper is titled *Transactional Correctness and Concurrency Control in Distributed Ledger Systems*.

---

## The Problems This Solves

Most ledger implementations handle the happy path well. The interesting questions are:

**What prevents a deadlock when two transfers touch the same accounts in opposite order?**
A naive implementation locks accounts in arrival order. Two concurrent transfers — A→B and B→A — deadlock immediately. This system enforces deterministic lock acquisition order by sorting account IDs before locking. The deadlock is structurally impossible.

**What prevents double-entry invariants from being violated by a bug in application code?**
Application-level checks can be bypassed. A direct SQL insert, a missed validation branch, or a partial rollback can all corrupt the ledger. This system enforces the double-entry constraint via a `DEFERRABLE` constraint trigger — the database itself refuses any transaction that would leave debits and credits unbalanced, regardless of how the write arrived.

**What prevents a client retry from processing a transfer twice?**
Idempotency keys alone are insufficient if they can collide or be generated inconsistently. This system uses SHA-256 hashing of the full request payload as the idempotency key, maintained in a state machine that tracks whether a request is pending, completed, or failed. A retry with the same payload returns the original result. A different payload with a collision is structurally impossible.

---

## Design Decisions

Full reasoning in [`/docs/adr/`](./docs/adr/). Summary:

| Decision | What was chosen | Why not the alternative |
|---|---|---|
| Concurrency control | Pessimistic locking with deterministic account ID ordering | Optimistic concurrency shifts deadlock risk to retry storms under contention |
| Invariant enforcement | `DEFERRABLE` constraint trigger at the database level | Application-level checks can be bypassed by any direct database write |
| Idempotency | SHA-256 request hashing + state machine | UUID keys require the client to generate and track them; hash is derived from payload |
| Contention behavior | `SELECT FOR UPDATE NOWAIT` — fail fast | Waiting under contention exhausts the connection pool; fail fast and let the caller retry |

---

## Benchmarks

Tested under two load profiles using [pgbench / custom load harness — *add your tooling*].

| Load profile | TPS | Invariant violations | Notes |
|---|---|---|---|
| Uniform load | 359 | 0 / 16,000+ requests | Deadlock-free across all concurrent transfers |
| Hotspot load | — | 0 | 66% abort rate — correct consistency-over-availability behavior |

The 66% abort rate under hotspot load is intentional and correct. When two transfers contend for the same accounts, one aborts immediately via `NOWAIT` rather than waiting. The caller retries. The invariants hold. This is the right tradeoff for a financial system.

---

## Architecture

```
Client
  │
  ▼
HTTP API (Go)
  │
  ├── Idempotency layer
  │     SHA-256(request) → check state machine
  │     If seen: return stored result
  │     If new: proceed
  │
  ▼
Transfer coordinator
  │
  ├── Sort account IDs (deterministic lock order)
  │
  ▼
PostgreSQL
  ├── SELECT FOR UPDATE NOWAIT on both accounts
  ├── Debit source, credit destination
  ├── DEFERRABLE trigger checks: sum(debits) = sum(credits)
  └── Commit or rollback
```

---

## Running It

Requires Docker and Go 1.21+.

```bash
git clone https://github.com/punchamoorthee/ledgerops
cd ledgerops

# Start Postgres
docker compose up -d

# Run migrations
make migrate

# Start the server
make run
```

The server runs on `localhost:8080`. See [`/docs/api.md`](./docs/api.md) for endpoint reference.

To run the benchmark suite:

```bash
make bench
```

---

## Repository Structure

```
/cmd          Entry point
/internal
  /ledger     Core transfer logic and coordinator
  /idempotency  State machine and request hashing
  /db         Migrations and query layer
/docs
  /adr        Architectural decision records
  api.md      API reference
```

---

## What's Next

Extending to two nodes with two-phase commit. The existing guarantees — the trigger, the idempotency layer, the locking — hold within a single Postgres instance. They break in specific and instructive ways when the coordinator and the data live on separate nodes. That work is in progress and will be documented here and on [my Substack](https://punchamoorthee.substack.com) as it breaks.

---

## Thesis

*LedgerOps — Transactional Correctness and Concurrency Control in Distributed Ledger Systems*
California State University, Dominguez Hills · May 2026

---

*Questions about the design decisions or the benchmarks: open an issue or reach out at punchamoorthee@gmail.com.*
