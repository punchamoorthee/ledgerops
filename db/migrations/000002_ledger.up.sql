-- 1. Drop the old, incorrect 'ledger_entries' table from migration 1 FIRST.
DROP TABLE "ledger_entries";

-- 2. Add the non-negative balance constraint to accounts.
ALTER TABLE "accounts" ADD CONSTRAINT "balance_non_negative" CHECK (balance >= 0);

-- 3. Create the 'transfers' table. This records the *intent* of a transfer.
CREATE TABLE
  "transfers" (
    "id" bigserial PRIMARY KEY,
    "from_account_id" bigint NOT NULL REFERENCES "accounts" ("id"),
    "to_account_id" bigint NOT NULL REFERENCES "accounts" ("id"),
    "amount" bigint NOT NULL CHECK (amount > 0),
    "status" text NOT NULL CHECK (status IN ('completed', 'failed')),
    "created_at" timestamptz NOT NULL DEFAULT (now ())
  );

-- 4. Create the new, correct 'ledger_entries' table. This is the "truth" of the ledger.
CREATE TABLE
  "ledger_entries" (
    "id" bigserial PRIMARY KEY,
    "transfer_id" bigint NOT NULL REFERENCES "transfers" ("id") ON DELETE CASCADE,
    "account_id" bigint NOT NULL REFERENCES "accounts" ("id"),
    "delta" bigint NOT NULL CHECK (delta <> 0),
    "created_at" timestamptz NOT NULL DEFAULT (now ())
  );

-- 5. Create the 'idempotency_keys' table. This is the state for the idempotency system.
CREATE TABLE
  "idempotency_keys" (
    "key" text PRIMARY KEY,
    "request_hash" text NOT NULL,
    "status" text NOT NULL CHECK (status IN ('in_progress', 'completed', 'failed')),
    "transfer_id" bigint NULL REFERENCES "transfers" ("id"),
    "response_status" int NULL,
    "response_body" jsonb NULL,
    "created_at" timestamptz NOT NULL DEFAULT (now ())
  );

-- 6. ADDED: Function and Trigger to enforce the SUM(delta) = 0 invariant.

CREATE
OR REPLACE FUNCTION check_ledger_invariant () RETURNS TRIGGER AS $$
BEGIN
  -- Check if the transfer is balanced
  IF (
    SELECT
      SUM(delta)
    FROM
      ledger_entries
    WHERE
      transfer_id = NEW.transfer_id
  ) <> 0 THEN
    RAISE EXCEPTION 'Ledger invariant violated: SUM(delta) for transfer_id % is not zero',
    NEW.transfer_id;
  END IF;
  
  -- Check if there are exactly two entries
  IF (
    SELECT
      COUNT(*)
    FROM
      ledger_entries
    WHERE
      transfer_id = NEW.transfer_id
  ) > 2 THEN
    RAISE EXCEPTION 'Ledger invariant violated: More than two entries for transfer_id %',
    NEW.transfer_id;
  END IF;

  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- This trigger calls the function.
-- It is DEFERRABLE and INITIALLY DEFERRED so it runs at the *end* of the transaction,
-- after both ledger entries have been inserted.
CREATE CONSTRAINT TRIGGER check_invariant_trigger
AFTER INSERT ON ledger_entries
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW
EXECUTE FUNCTION check_ledger_invariant ();