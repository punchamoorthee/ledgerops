#!/bin/bash
set -e

echo "=== LedgerOps Benchmarking Suite ==="
echo ""

# Configuration
DURATION=${1:-60s}
WORKERS=${2:-10}

# Ensure clean state
echo "Resetting database..."
make migrate-down || true
make migrate-up
echo "✓ Database reset"
echo ""

# Run uniform workload
echo "Running Uniform Workload (Low Contention)..."
echo "  Duration: $DURATION"
echo "  Workers: $WORKERS"
./bin/benchmark -workload=uniform -duration=$DURATION -workers=$WORKERS -output=results_uniform.json
echo "✓ Uniform workload complete"
echo ""

# Verify integrity
echo "Verifying data integrity..."
make verify
echo "✓ Integrity verified"
echo ""

# Reset for hot-spot
echo "Resetting for hot-spot workload..."
make migrate-down
make migrate-up
echo ""

# Run hot-spot workload
echo "Running Hot-Spot Workload (High Contention)..."
echo "  Duration: $DURATION"
echo "  Workers: $WORKERS"
./bin/benchmark -workload=hotspot -duration=$DURATION -workers=$WORKERS -output=results_hotspot.json
echo "✓ Hot-spot workload complete"
echo ""

# Verify integrity again
echo "Verifying data integrity..."
make verify
echo "✓ Integrity verified"
echo ""

echo "=== Benchmark Complete ==="
echo ""
echo "Results:"
echo "  - results_uniform.json"
echo "  - results_hotspot.json"
echo ""