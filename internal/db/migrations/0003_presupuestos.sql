-- +up
CREATE TABLE budgets (
    id           TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    category_id  TEXT NOT NULL,
    amount_cents INTEGER NOT NULL CHECK (amount_cents > 0 AND amount_cents <= 100000000),
    thresholds   TEXT NOT NULL DEFAULT '50,80,100',
    active       INTEGER NOT NULL DEFAULT 1,
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL,
    UNIQUE (user_id, category_id),
    FOREIGN KEY (category_id, user_id) REFERENCES categories (id, user_id) ON DELETE CASCADE
);

-- Un aviso por presupuesto, mes y umbral: la clave primaria garantiza "una sola vez".
CREATE TABLE budget_alerts (
    budget_id   TEXT NOT NULL REFERENCES budgets (id) ON DELETE CASCADE,
    month       TEXT NOT NULL,
    threshold   INTEGER NOT NULL,
    spent_cents INTEGER NOT NULL,
    created_at  TEXT NOT NULL,
    notified_at TEXT,
    PRIMARY KEY (budget_id, month, threshold)
);

-- +down
DROP TABLE budget_alerts;
DROP TABLE budgets;
