-- +up
CREATE TABLE notification_preferences (
    user_id TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    budgets INTEGER NOT NULL DEFAULT 1 CHECK(budgets IN (0,1)),
    security INTEGER NOT NULL DEFAULT 1 CHECK(security IN (0,1)),
    weekly INTEGER NOT NULL DEFAULT 1 CHECK(weekly IN (0,1)),
    monthly INTEGER NOT NULL DEFAULT 1 CHECK(monthly IN (0,1)),
    hide_amounts INTEGER NOT NULL DEFAULT 1 CHECK(hide_amounts IN (0,1)),
    quiet_enabled INTEGER NOT NULL DEFAULT 0 CHECK(quiet_enabled IN (0,1)),
    quiet_start INTEGER NOT NULL DEFAULT 1320 CHECK(quiet_start BETWEEN 0 AND 1439),
    quiet_end INTEGER NOT NULL DEFAULT 480 CHECK(quiet_end BETWEEN 0 AND 1439)
);
CREATE TABLE push_subscriptions (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    endpoint_hash BLOB NOT NULL UNIQUE,
    subscription_enc BLOB NOT NULL,
    device_label TEXT NOT NULL,
    security_cursor INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    UNIQUE(id,user_id)
);
CREATE INDEX push_subscriptions_user ON push_subscriptions(user_id);
CREATE TABLE push_deliveries (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    subscription_id TEXT NOT NULL,
    event_key TEXT NOT NULL,
    kind TEXT NOT NULL,
    payload_enc BLOB NOT NULL,
    attempts INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    completed_at TEXT,
    UNIQUE(subscription_id,event_key),
    FOREIGN KEY(subscription_id,user_id) REFERENCES push_subscriptions(id,user_id) ON DELETE CASCADE
);
CREATE INDEX push_deliveries_due ON push_deliveries(next_attempt_at) WHERE completed_at IS NULL;
-- +down
DROP TABLE push_deliveries;
DROP TABLE push_subscriptions;
DROP TABLE notification_preferences;
