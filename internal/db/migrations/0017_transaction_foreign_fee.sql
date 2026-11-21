-- +up
ALTER TABLE transactions ADD COLUMN foreign_fee_cents INTEGER NOT NULL DEFAULT 0 CHECK (foreign_fee_cents >= 0);

-- +down
ALTER TABLE transactions DROP COLUMN foreign_fee_cents;
