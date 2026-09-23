CREATE UNIQUE INDEX IF NOT EXISTS uq_processed_reversal_reference
  ON wagering_transactions(provider_id, reference_external_id, kind)
  WHERE status = 'PROCESSED' AND kind IN ('REFUND', 'ROLLBACK');
