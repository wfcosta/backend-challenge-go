ALTER TABLE wagering_transactions
  ADD COLUMN IF NOT EXISTS reference_locked_until TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_wagering_reference_lease
  ON wagering_transactions(reference_locked_until)
  WHERE status = 'PENDING_REFERENCE';
