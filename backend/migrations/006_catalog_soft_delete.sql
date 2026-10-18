ALTER TABLE model_products ADD COLUMN deleted_at timestamptz;
ALTER TABLE payment_tiers ADD COLUMN deleted_at timestamptz;
