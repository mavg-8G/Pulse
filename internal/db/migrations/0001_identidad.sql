-- +up
CREATE TABLE users (
    id            TEXT PRIMARY KEY,
    email         TEXT NOT NULL UNIQUE COLLATE NOCASE,
    password_hash TEXT NOT NULL,
    role          TEXT NOT NULL CHECK (role IN ('admin', 'user')),
    dek_enc       BLOB NOT NULL,
    timezone      TEXT NOT NULL,
    theme         TEXT NOT NULL DEFAULT 'system' CHECK (theme IN ('system', 'light', 'dark')),
    failed_logins INTEGER NOT NULL DEFAULT 0,
    locked_until  TEXT,
    disabled_at   TEXT,
    created_at    TEXT NOT NULL
);

CREATE TABLE invites (
    id         TEXT PRIMARY KEY,
    code_hash  BLOB NOT NULL UNIQUE,
    email      TEXT COLLATE NOCASE,
    role       TEXT NOT NULL DEFAULT 'user' CHECK (role IN ('admin', 'user')),
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    used_at    TEXT,
    used_by    TEXT REFERENCES users (id) ON DELETE SET NULL,
    revoked_at TEXT
);

CREATE TABLE sessions (
    id                  TEXT PRIMARY KEY,           -- id público (para listar/revocar)
    token_hash          BLOB NOT NULL UNIQUE,       -- HMAC del token; el token nunca se guarda
    user_id             TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    state               TEXT NOT NULL CHECK (state IN ('enroll', 'mfa', 'full')),
    remember            INTEGER NOT NULL DEFAULT 0,
    ip                  TEXT NOT NULL DEFAULT '',
    user_agent          TEXT NOT NULL DEFAULT '',
    created_at          TEXT NOT NULL,
    last_seen_at        TEXT NOT NULL,
    expires_at          TEXT NOT NULL,              -- vencimiento por inactividad
    absolute_expires_at TEXT NOT NULL,
    stepup_at           TEXT
);
CREATE INDEX sessions_user ON sessions (user_id);

CREATE TABLE auth_challenges (
    session_id TEXT NOT NULL REFERENCES sessions (id) ON DELETE CASCADE,
    purpose    TEXT NOT NULL,
    data       BLOB NOT NULL,
    expires_at TEXT NOT NULL,
    PRIMARY KEY (session_id, purpose)
);

CREATE TABLE totp (
    user_id      TEXT PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    secret_enc   BLOB NOT NULL,
    confirmed_at TEXT,
    last_step    INTEGER NOT NULL DEFAULT 0            -- evita reutilizar el mismo código
);

CREATE TABLE webauthn_credentials (
    id            TEXT PRIMARY KEY,
    user_id       TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    credential_id BLOB NOT NULL UNIQUE,
    public_key    BLOB NOT NULL,                       -- clave COSE
    alg           INTEGER NOT NULL,
    sign_count    INTEGER NOT NULL DEFAULT 0,
    name          TEXT NOT NULL,
    created_at    TEXT NOT NULL,
    last_used_at  TEXT
);
CREATE INDEX webauthn_user ON webauthn_credentials (user_id);

CREATE TABLE recovery_codes (
    user_id   TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    code_hash BLOB NOT NULL,
    used_at   TEXT,
    PRIMARY KEY (user_id, code_hash)
);

CREATE TABLE audit_log (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id    TEXT,
    event      TEXT NOT NULL,
    ip         TEXT NOT NULL DEFAULT '',
    user_agent TEXT NOT NULL DEFAULT '',
    meta       TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL
);
CREATE INDEX audit_user ON audit_log (user_id, id);
CREATE TRIGGER audit_log_no_update BEFORE UPDATE ON audit_log
BEGIN SELECT RAISE(ABORT, 'audit_log es solo de inserción'); END;
CREATE TRIGGER audit_log_no_delete BEFORE DELETE ON audit_log
BEGIN SELECT RAISE(ABORT, 'audit_log es solo de inserción'); END;

-- +down
DROP TABLE audit_log;
DROP TABLE recovery_codes;
DROP TABLE webauthn_credentials;
DROP TABLE totp;
DROP TABLE auth_challenges;
DROP TABLE sessions;
DROP TABLE invites;
DROP TABLE users;
