CREATE EXTENSION IF NOT EXISTS pgcrypto;
CREATE TABLE wallets (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  player_id UUID NOT NULL,
  currency CHAR(3) NOT NULL,
  balance_minor BIGINT NOT NULL CHECK (balance_minor >= 0),
  version BIGINT NOT NULL CHECK (version >= 1),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (player_id, currency)
);
CREATE TABLE wagering_transactions (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  provider_id TEXT,
  external_transaction_id TEXT,
  idempotency_key TEXT,
  payload_hash TEXT,
  wallet_id UUID NOT NULL REFERENCES wallets(id),
  player_id UUID NOT NULL,
  round_id TEXT,
  game_id TEXT,
  kind TEXT NOT NULL CHECK (kind IN ('OPENING','BET','WIN','LOSS','REFUND','ROLLBACK')),
  amount_minor BIGINT NOT NULL CHECK (amount_minor >= 0),
  currency CHAR(3) NOT NULL,
  reference_external_id TEXT,
  reference_transaction_id UUID REFERENCES wagering_transactions(id),
  status TEXT NOT NULL CHECK (status IN ('PENDING','PENDING_REFERENCE','PROCESSED','REJECTED','FAILED')),
  failure_code TEXT,
  result_balance_minor BIGINT,
  result_wallet_version BIGINT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (provider_id, external_transaction_id),
  UNIQUE (idempotency_key)
);
CREATE TABLE ledger_entries (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  wallet_id UUID NOT NULL REFERENCES wallets(id),
  transaction_id UUID NOT NULL UNIQUE REFERENCES wagering_transactions(id),
  direction TEXT NOT NULL CHECK (direction IN ('DEBIT','CREDIT')),
  amount_minor BIGINT NOT NULL CHECK (amount_minor > 0),
  balance_before_minor BIGINT NOT NULL CHECK (balance_before_minor >= 0),
  balance_after_minor BIGINT NOT NULL CHECK (balance_after_minor >= 0),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CHECK ((direction = 'DEBIT' AND balance_after_minor = balance_before_minor - amount_minor)
      OR (direction = 'CREDIT' AND balance_after_minor = balance_before_minor + amount_minor))
);
CREATE TABLE inbox_messages (
  consumer_name TEXT NOT NULL,
  message_id TEXT NOT NULL,
  payload_hash TEXT NOT NULL,
  received_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  completed_at TIMESTAMPTZ,
  PRIMARY KEY (consumer_name, message_id)
);
CREATE TABLE outbox_events (
  event_id UUID PRIMARY KEY,
  aggregate_id UUID NOT NULL,
  event_type TEXT NOT NULL,
  payload JSONB NOT NULL,
  occurred_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  attempts INT NOT NULL DEFAULT 0,
  next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  locked_until TIMESTAMPTZ,
  published_at TIMESTAMPTZ
);
CREATE OR REPLACE FUNCTION reject_ledger_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'ledger is append-only'; END; $$;
CREATE TRIGGER ledger_immutable BEFORE UPDATE OR DELETE ON ledger_entries
FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
