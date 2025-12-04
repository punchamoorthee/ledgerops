CREATE TABLE "accounts" (
  "id" bigserial PRIMARY KEY,
  "balance" bigint NOT NULL DEFAULT 0 CHECK (balance >= 0),
  "created_at" timestamptz NOT NULL DEFAULT (now())
);

CREATE TABLE "transfers" (
  "id" bigserial PRIMARY KEY,
  "from_account_id" bigint NOT NULL REFERENCES "accounts" ("id"),
  "to_account_id" bigint NOT NULL REFERENCES "accounts" ("id"),
  "amount" bigint NOT NULL CHECK (amount > 0),
  "status" text NOT NULL CHECK (status IN ('completed', 'failed')),
  "created_at" timestamptz NOT NULL DEFAULT (now())
);

CREATE TABLE "ledger_entries" (
  "id" bigserial PRIMARY KEY,
  "transfer_id" bigint NOT NULL REFERENCES "transfers" ("id") ON DELETE CASCADE,
  "account_id" bigint NOT NULL REFERENCES "accounts" ("id"),
  "delta" bigint NOT NULL CHECK (delta <> 0),
  "created_at" timestamptz NOT NULL DEFAULT (now())
);

CREATE TABLE "idempotency_keys" (
  "key" text PRIMARY KEY,
  "request_hash" text NOT NULL,
  "status" text NOT NULL CHECK (status IN ('in_progress', 'completed', 'failed')),
  "transfer_id" bigint NULL REFERENCES "transfers" ("id"),
  "response_status" int NULL,
  "response_body" jsonb NULL,
  "created_at" timestamptz NOT NULL DEFAULT (now())
);

-- Invariant Trigger
CREATE OR REPLACE FUNCTION check_ledger_invariant() RETURNS TRIGGER AS $$
BEGIN
  IF (SELECT SUM(delta) FROM ledger_entries WHERE transfer_id = NEW.transfer_id) <> 0 THEN
    RAISE EXCEPTION 'Ledger invariant violated: SUM(delta) <> 0';
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE CONSTRAINT TRIGGER check_invariant_trigger
AFTER INSERT ON ledger_entries
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW
EXECUTE FUNCTION check_ledger_invariant();