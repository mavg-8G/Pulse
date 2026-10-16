-- +up
-- Fase 12: historial de respaldos cifrados enviados fuera del servidor. Solo metadatos: nombre, tamaño
-- y huella SHA-256 del archivo cifrado; nunca contenido ni credenciales.
CREATE TABLE backup_runs (
    id          TEXT PRIMARY KEY,
    trigger     TEXT NOT NULL CHECK (trigger IN ('manual', 'scheduled')),
    user_id     TEXT REFERENCES users (id) ON DELETE SET NULL, -- quién lo pidió (manual)
    started_at  TEXT NOT NULL,
    finished_at TEXT NOT NULL,
    status      TEXT NOT NULL CHECK (status IN ('ok', 'failed')),
    name        TEXT NOT NULL DEFAULT '',
    size_bytes  INTEGER NOT NULL DEFAULT 0,
    sha256      TEXT NOT NULL DEFAULT '',
    attempts    INTEGER NOT NULL DEFAULT 0,
    error       TEXT NOT NULL DEFAULT ''
);
CREATE INDEX backup_runs_started ON backup_runs (started_at);

-- +down
DROP TABLE backup_runs;
