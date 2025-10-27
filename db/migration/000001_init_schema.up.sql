CREATE TABLE "accounts" (
  "id" bigserial PRIMARY KEY,
  "balance" bigint NOT NULL DEFAULT 0,
  "created_at" timestamptz NOT NULL DEFAULT (now())
);

CREATE TABLE "ledger_entries" (
  "id" bigserial PRIMARY KEY,
  "debit_account_id" bigint NOT NULL,
  "credit_account_id" bigint NOT NULL,
  "amount" bigint NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT (now()),
  CONSTRAINT "positive_amount" CHECK (amount > 0)
);

ALTER TABLE "ledger_entries" ADD FOREIGN KEY ("debit_account_id") REFERENCES "accounts" ("id");
ALTER TABLE "ledger_entries" ADD FOREIGN KEY ("credit_account_id") REFERENCES "accounts" ("id");