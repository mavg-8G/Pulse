-- +up
-- 1) Precisión del modo sombra: qué pasó al revisar un gasto importado.
--    '' = sin revisar, 'ok' = se confirmó tal cual, 'fixed' = hubo que corregir monto, fecha, tipo o cuenta.
ALTER TABLE transactions ADD COLUMN review TEXT NOT NULL DEFAULT '';

-- 2) Detector de plantillas cambiadas: correos de un banco conocido que ningún parser entendió.
--    Solo el asunto (cifrado) y un hash del mensaje; el cuerpo nunca se guarda.
CREATE TABLE template_alerts (
    id          TEXT PRIMARY KEY,
    user_id     TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    parser      TEXT NOT NULL,
    subject_enc BLOB,
    msg_hash    BLOB NOT NULL,
    created_at  TEXT NOT NULL,
    UNIQUE (user_id, msg_hash)
);
CREATE INDEX template_alerts_user_date ON template_alerts (user_id, created_at);

-- 3) Conciliación: total del estado de cuenta que el usuario ingresa a mano, por cuenta y mes.
CREATE TABLE statements (
    user_id     TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    account_id  TEXT NOT NULL,
    month       TEXT NOT NULL, -- AAAA-MM
    total_cents INTEGER NOT NULL CHECK (total_cents >= 0 AND total_cents <= 100000000),
    updated_at  TEXT NOT NULL,
    PRIMARY KEY (user_id, account_id, month),
    FOREIGN KEY (account_id, user_id) REFERENCES accounts (id, user_id) ON DELETE CASCADE
);

-- 4) Gastos recurrentes: se registran solos cada mes en el día elegido.
CREATE TABLE recurring (
    id           TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    account_id   TEXT NOT NULL,
    category_id  TEXT NOT NULL,
    amount_cents INTEGER NOT NULL CHECK (amount_cents > 0 AND amount_cents <= 100000000),
    merchant_enc BLOB,
    day          INTEGER NOT NULL CHECK (day BETWEEN 1 AND 31),
    next_date    TEXT NOT NULL, -- AAAA-MM-DD en la zona del usuario
    created_at   TEXT NOT NULL,
    FOREIGN KEY (account_id, user_id) REFERENCES accounts (id, user_id) ON DELETE CASCADE,
    FOREIGN KEY (category_id, user_id) REFERENCES categories (id, user_id) ON DELETE CASCADE
);
CREATE INDEX recurring_next ON recurring (next_date);

-- +down
DROP TABLE recurring;
DROP TABLE statements;
DROP TABLE template_alerts;
ALTER TABLE transactions DROP COLUMN review;
