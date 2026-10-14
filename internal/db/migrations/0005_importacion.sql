-- +up
-- 1) Los gastos pueden venir de un correo importado a mano (source = 'email').
--    SQLite no permite cambiar un CHECK: se reconstruye la tabla con los mismos datos.
CREATE TABLE transactions_v5 (
    id              TEXT PRIMARY KEY,
    user_id         TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    account_id      TEXT NOT NULL,
    category_id     TEXT NOT NULL,
    amount_cents    INTEGER NOT NULL CHECK (amount_cents > 0 AND amount_cents <= 100000000),
    currency        TEXT NOT NULL DEFAULT 'USD',
    occurred_at     TEXT NOT NULL,
    kind            TEXT NOT NULL DEFAULT 'purchase' CHECK (kind IN ('purchase', 'refund')),
    merchant_enc    BLOB,
    description_enc BLOB,
    notes_enc       BLOB,
    source          TEXT NOT NULL DEFAULT 'manual' CHECK (source IN ('manual', 'gmail', 'email')),
    status          TEXT NOT NULL DEFAULT 'confirmed' CHECK (status IN ('confirmed', 'pending_review')),
    message_hash    BLOB,
    parser          TEXT NOT NULL DEFAULT '',
    confidence      REAL,
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL,
    FOREIGN KEY (account_id, user_id) REFERENCES accounts (id, user_id),
    FOREIGN KEY (category_id, user_id) REFERENCES categories (id, user_id),
    UNIQUE (user_id, message_hash)
);
INSERT INTO transactions_v5 SELECT * FROM transactions;
DROP TABLE transactions;
ALTER TABLE transactions_v5 RENAME TO transactions;
CREATE INDEX transactions_user_date ON transactions (user_id, occurred_at);
CREATE INDEX transactions_category_date ON transactions (category_id, occurred_at);
CREATE INDEX transactions_account ON transactions (account_id);

-- 2) Reglas "este comercio siempre va a esta categoría". El patrón va cifrado;
--    pattern_hash (HMAC) evita duplicados sin guardarlo en claro.
CREATE TABLE merchant_rules (
    id           TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    pattern_enc  BLOB NOT NULL,
    pattern_hash BLOB NOT NULL,
    category_id  TEXT NOT NULL,
    created_at   TEXT NOT NULL,
    UNIQUE (user_id, pattern_hash),
    FOREIGN KEY (category_id, user_id) REFERENCES categories (id, user_id) ON DELETE CASCADE
);

-- +down
DROP TABLE merchant_rules;
CREATE TABLE transactions_v4 (
    id              TEXT PRIMARY KEY,
    user_id         TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    account_id      TEXT NOT NULL,
    category_id     TEXT NOT NULL,
    amount_cents    INTEGER NOT NULL CHECK (amount_cents > 0 AND amount_cents <= 100000000),
    currency        TEXT NOT NULL DEFAULT 'USD',
    occurred_at     TEXT NOT NULL,
    kind            TEXT NOT NULL DEFAULT 'purchase' CHECK (kind IN ('purchase', 'refund')),
    merchant_enc    BLOB,
    description_enc BLOB,
    notes_enc       BLOB,
    source          TEXT NOT NULL DEFAULT 'manual' CHECK (source IN ('manual', 'gmail')),
    status          TEXT NOT NULL DEFAULT 'confirmed' CHECK (status IN ('confirmed', 'pending_review')),
    message_hash    BLOB,
    parser          TEXT NOT NULL DEFAULT '',
    confidence      REAL,
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL,
    FOREIGN KEY (account_id, user_id) REFERENCES accounts (id, user_id),
    FOREIGN KEY (category_id, user_id) REFERENCES categories (id, user_id),
    UNIQUE (user_id, message_hash)
);
INSERT INTO transactions_v4 SELECT id, user_id, account_id, category_id, amount_cents, currency, occurred_at, kind, merchant_enc,
    description_enc, notes_enc, CASE source WHEN 'email' THEN 'manual' ELSE source END, status, message_hash, parser, confidence,
    created_at, updated_at FROM transactions;
DROP TABLE transactions;
ALTER TABLE transactions_v4 RENAME TO transactions;
CREATE INDEX transactions_user_date ON transactions (user_id, occurred_at);
CREATE INDEX transactions_category_date ON transactions (category_id, occurred_at);
CREATE INDEX transactions_account ON transactions (account_id);
