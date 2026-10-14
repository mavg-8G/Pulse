-- +up
CREATE TABLE accounts (
    id          TEXT PRIMARY KEY,
    user_id     TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    type        TEXT NOT NULL CHECK (type IN ('bank', 'credit_card', 'debit_card', 'cash', 'wallet')),
    institution TEXT NOT NULL DEFAULT '',
    card_suffix TEXT NOT NULL DEFAULT '',
    currency    TEXT NOT NULL DEFAULT 'USD',
    color       TEXT NOT NULL DEFAULT '#64748b',
    icon        TEXT NOT NULL DEFAULT '',
    archived    INTEGER NOT NULL DEFAULT 0,
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL,
    UNIQUE (id, user_id)
);
CREATE INDEX accounts_user ON accounts (user_id);

CREATE TABLE categories (
    id         TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    parent_id  TEXT,
    name       TEXT NOT NULL,
    color      TEXT NOT NULL DEFAULT '#64748b',
    icon       TEXT NOT NULL DEFAULT '',
    is_system  INTEGER NOT NULL DEFAULT 0,
    archived   INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE (id, user_id),
    -- el padre debe ser del mismo usuario (integridad multiusuario a nivel de BD)
    FOREIGN KEY (parent_id, user_id) REFERENCES categories (id, user_id)
);
CREATE UNIQUE INDEX categories_unique_name ON categories (user_id, COALESCE(parent_id, ''), name COLLATE NOCASE);

CREATE TABLE transactions (
    id              TEXT PRIMARY KEY,
    user_id         TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    account_id      TEXT NOT NULL,
    category_id     TEXT NOT NULL,
    amount_cents    INTEGER NOT NULL CHECK (amount_cents > 0 AND amount_cents <= 100000000),
    currency        TEXT NOT NULL DEFAULT 'USD',
    occurred_at     TEXT NOT NULL,                     -- UTC, formato fijo ordenable
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
CREATE INDEX transactions_user_date ON transactions (user_id, occurred_at);
CREATE INDEX transactions_category_date ON transactions (category_id, occurred_at);
CREATE INDEX transactions_account ON transactions (account_id);

-- +down
DROP TABLE transactions;
DROP TABLE categories;
DROP TABLE accounts;
