-- +up
CREATE TABLE exchange_rate_cache (
    key_hash BLOB PRIMARY KEY NOT NULL,
    attempted_at INTEGER NOT NULL,
    finished INTEGER NOT NULL DEFAULT 0,
    rate TEXT NOT NULL DEFAULT '',
    quote_date TEXT NOT NULL DEFAULT '',
    expires_at INTEGER NOT NULL DEFAULT 0
);

-- +down
DROP TABLE exchange_rate_cache;
