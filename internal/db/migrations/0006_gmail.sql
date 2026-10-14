-- +up
-- Conexión de Gmail por usuario. Todo lo que identifica a la persona va cifrado con su DEK;
-- email_hash (HMAC) permite encontrar al usuario cuando llega un aviso de Pub/Sub.
CREATE TABLE gmail_connections (
    user_id          TEXT PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    email_enc        BLOB NOT NULL,
    email_hash       BLOB NOT NULL UNIQUE,
    refresh_enc      BLOB NOT NULL,
    status           TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'broken')),
    last_error       TEXT NOT NULL DEFAULT '',
    history_id       INTEGER NOT NULL DEFAULT 0,
    last_sync_at     TEXT,
    last_ok_at       TEXT,
    next_sync_at     TEXT NOT NULL,
    failures         INTEGER NOT NULL DEFAULT 0,
    watch_expires_at TEXT,
    shadow           INTEGER NOT NULL DEFAULT 1 CHECK (shadow IN (0, 1)),
    gemini           INTEGER NOT NULL DEFAULT 0 CHECK (gemini IN (0, 1)),
    imported         INTEGER NOT NULL DEFAULT 0,
    created_at       TEXT NOT NULL,
    updated_at       TEXT NOT NULL
);
CREATE INDEX gmail_connections_due ON gmail_connections (next_sync_at) WHERE status = 'active';

-- Mensajes ya procesados (HMAC del id de Gmail): evita volver a descargarlos. No guarda el correo.
CREATE TABLE gmail_seen (
    user_id  TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    msg_hash BLOB NOT NULL,
    seen_at  TEXT NOT NULL,
    PRIMARY KEY (user_id, msg_hash)
) WITHOUT ROWID;

-- +down
DROP TABLE gmail_seen;
DROP TABLE gmail_connections;
