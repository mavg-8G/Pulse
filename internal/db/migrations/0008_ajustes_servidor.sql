-- +up
-- Ajustes del servidor editables desde la app (admin). Los valores secretos van cifrados.
CREATE TABLE server_settings (
    key TEXT PRIMARY KEY,
    value BLOB NOT NULL,
    updated_at TEXT NOT NULL
);

-- +down
DROP TABLE server_settings;
