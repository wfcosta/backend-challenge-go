ALTER TABLE wagering_transactions
  ADD COLUMN IF NOT EXISTS reference_attempts INT NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS reference_next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  ADD COLUMN IF NOT EXISTS reference_expires_at TIMESTAMPTZ NOT NULL DEFAULT (now() + INTERVAL '15 minutes');

CREATE INDEX IF NOT EXISTS idx_wagering_pending_reference_retry
  ON wagering_transactions(status, reference_next_attempt_at)
  WHERE status = 'PENDING_REFERENCE';
