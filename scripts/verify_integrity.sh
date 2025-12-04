#!/bin/bash
set -e

echo "=== LedgerOps Integrity Verification ==="
echo ""

DB_URL="postgresql://postgres:secret@localhost:5432/ledgerops?sslmode=disable"

# Check 1: Sum of all ledger entries
echo "Check 1: Sum of all ledger entries must equal zero"
SUM=$(psql $DB_URL -t -c "SELECT SUM(debit - credit) FROM ledger_entries;")
if [ "$SUM" -eq "0" ]; then
    echo "✓ PASS: Sum = 0 (money conserved)"
else
    echo "✗ FAIL: Sum = $SUM (money created or destroyed!)"
    exit 1
fi
echo ""

# Check 2: All account balances non-negative
echo "Check 2: All account balances must be non-negative"
NEGATIVE=$(psql $DB_URL -t -c "SELECT COUNT(*) FROM accounts WHERE balance < 0;")
if [ "$NEGATIVE" -eq "0" ]; then
    echo "✓ PASS: No negative balances"
else
    echo "✗ FAIL: Found $NEGATIVE accounts with negative balance"
    exit 1
fi
echo ""

# Check 3: Every transfer has exactly 2 ledger entries
echo "Check 3: Every transfer must have exactly 2 ledger entries"
INVALID=$(psql $DB_URL -t -c "SELECT COUNT(*) FROM transfers t WHERE (SELECT COUNT(*) FROM ledger_entries WHERE transfer_id = t.id) != 2;")
if [ "$INVALID" -eq "0" ]; then
    echo "✓ PASS: All transfers have 2 ledger entries"
else
    echo "✗ FAIL: Found $INVALID transfers with incorrect entry count"
    exit 1
fi
echo ""

# Summary
TRANSFER_COUNT=$(psql $DB_URL -t -c "SELECT COUNT(*) FROM transfers;")
ENTRY_COUNT=$(psql $DB_URL -t -c "SELECT COUNT(*) FROM ledger_entries;")

echo "=== Summary ==="
echo "  Total Transfers: $TRANSFER_COUNT"
echo "  Total Ledger Entries: $ENTRY_COUNT"
echo "  All Integrity Checks: PASSED ✓"
echo ""