-- 1. Drop the new tables.
DROP TABLE "idempotency_keys";

DROP TABLE "ledger_entries";

DROP TABLE "transfers";

-- 2. Drop the constraint from the 'accounts' table.
ALTER TABLE "accounts"
DROP CONSTRAINT "balance_non_negative";

-- 3. Re-create the old, incorrect ledger_entries table.
CREATE TABLE
  "ledger_entries" (
    "id" bigserial PRIMARY KEY,
    "debit_account_id" bigint NOT NULL REFERENCES "accounts" ("id"),
    "credit_account_id" bigint NOT NULL REFERENCES "accounts" ("id"),
    "amount" bigint NOT NULL,
    "created_at" timestamptz NOT NULL DEFAULT (now ()),
    CONSTRAINT "positive_amount" CHECK (amount > 0)
  );