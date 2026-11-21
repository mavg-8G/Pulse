-- +up
CREATE TABLE exchange_settings (
    user_id TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    api_key_enc BLOB NOT NULL
);

-- +down
DROP TABLE exchange_settings;
