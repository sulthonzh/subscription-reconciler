CREATE TABLE entitlements (
    user_id       TEXT NOT NULL,
    source        TEXT NOT NULL,
    active        BOOLEAN NOT NULL DEFAULT FALSE,
    expires_at    TEXT,
    reason        TEXT,
    last_changed_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    last_event_time_ms INTEGER NOT NULL DEFAULT 0,
    created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    PRIMARY KEY (user_id, source)
);

CREATE TABLE store_events (
    event_id      TEXT PRIMARY KEY,
    user_id       TEXT    NOT NULL,
    type          TEXT    NOT NULL,
    event_time_ms INTEGER NOT NULL,
    product_id    TEXT    NOT NULL,
    processed_at  TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE carrier_poll_log (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id       TEXT    NOT NULL,
    polled_at     TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    status        TEXT    NOT NULL,
    locked_until  TEXT
);

CREATE TABLE notifications (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id       TEXT NOT NULL,
    type          TEXT NOT NULL DEFAULT 'PREMIUM_EXPIRES_SOON',
    scheduled_for TEXT NOT NULL,
    sent_at       TEXT,
    created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    UNIQUE(user_id, type, scheduled_for)
);

CREATE TABLE IF NOT EXISTS audit_log (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id       TEXT NOT NULL,
    trigger_id    TEXT,
    source        TEXT NOT NULL,
    previous_state TEXT NOT NULL,
    next_state    TEXT NOT NULL,
    created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

PRAGMA journal_mode = WAL;
PRAGMA busy_timeout = 5000;
PRAGMA foreign_keys = ON;
PRAGMA strict = ON;
