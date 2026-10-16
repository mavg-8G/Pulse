-- +up
-- Fase 11: presupuestos por cuenta además de por categoría, arrastre del sobrante o exceso al mes siguiente
-- y topes distintos para un mes concreto. SQLite no permite volver opcional category_id con ALTER, así que
-- se reconstruye la tabla conservando ids, montos, umbrales y estado. Borrar la tabla vieja borra en cascada
-- los avisos ya registrados: se copian antes y se reponen, para que ningún aviso se repita.
CREATE TEMP TABLE budget_alerts_respaldo AS SELECT * FROM budget_alerts;
CREATE TABLE budgets_nueva (
    id            TEXT PRIMARY KEY,
    user_id       TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    category_id   TEXT,
    account_id    TEXT,
    amount_cents  INTEGER NOT NULL CHECK (amount_cents > 0 AND amount_cents <= 100000000),
    thresholds    TEXT NOT NULL DEFAULT '50,80,100',
    active        INTEGER NOT NULL DEFAULT 1,
    rollover      INTEGER NOT NULL DEFAULT 0,
    rollover_from TEXT NOT NULL DEFAULT '', -- AAAA-MM: primer mes cuyo sobrante o exceso pasa al siguiente
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL,
    CHECK ((category_id IS NULL) != (account_id IS NULL)), -- exactamente uno: categoría o cuenta
    UNIQUE (user_id, category_id),
    UNIQUE (user_id, account_id),
    FOREIGN KEY (category_id, user_id) REFERENCES categories (id, user_id) ON DELETE CASCADE,
    FOREIGN KEY (account_id, user_id) REFERENCES accounts (id, user_id) ON DELETE CASCADE
);
INSERT INTO budgets_nueva (id, user_id, category_id, amount_cents, thresholds, active, created_at, updated_at)
SELECT id, user_id, category_id, amount_cents, thresholds, active, created_at, updated_at FROM budgets;
DROP TABLE budgets;
ALTER TABLE budgets_nueva RENAME TO budgets;
INSERT INTO budget_alerts SELECT * FROM budget_alerts_respaldo;
DROP TABLE budget_alerts_respaldo;

-- Tope de un mes concreto (por ejemplo, diciembre). Sin fila, vale el tope general del presupuesto.
CREATE TABLE budget_months (
    budget_id    TEXT NOT NULL REFERENCES budgets (id) ON DELETE CASCADE,
    month        TEXT NOT NULL, -- AAAA-MM
    amount_cents INTEGER NOT NULL CHECK (amount_cents > 0 AND amount_cents <= 100000000),
    PRIMARY KEY (budget_id, month)
);

-- +down
-- Bajar de versión descarta los presupuestos por cuenta, los topes por mes y el arrastre.
DROP TABLE budget_months;
CREATE TEMP TABLE budget_alerts_respaldo AS
SELECT a.* FROM budget_alerts a JOIN budgets b ON b.id = a.budget_id WHERE b.category_id IS NOT NULL;
CREATE TABLE budgets_vieja (
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
INSERT INTO budgets_vieja SELECT id, user_id, category_id, amount_cents, thresholds, active, created_at, updated_at
FROM budgets WHERE category_id IS NOT NULL;
DROP TABLE budgets;
ALTER TABLE budgets_vieja RENAME TO budgets;
INSERT INTO budget_alerts SELECT * FROM budget_alerts_respaldo;
DROP TABLE budget_alerts_respaldo;
