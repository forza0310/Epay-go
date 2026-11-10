CREATE TABLE owner (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    password_hash TEXT NOT NULL,
    created_at INTEGER NOT NULL
);

CREATE TABLE admin_sessions (
    token_hash BLOB PRIMARY KEY,
    csrf_hash BLOB NOT NULL,
    created_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL
);

CREATE INDEX admin_sessions_expiry ON admin_sessions(expires_at);

CREATE TABLE admin_login_attempts (
    remote_key TEXT PRIMARY KEY,
    attempts INTEGER NOT NULL,
    window_started INTEGER NOT NULL,
    locked_until INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE channels (
    id TEXT PRIMARY KEY,
    plugin_id TEXT NOT NULL,
    account_id TEXT NOT NULL,
    name TEXT NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 0 CHECK (enabled IN (0, 1)),
    config_ciphertext BLOB NOT NULL
);

CREATE TABLE orders (
    trade_no TEXT PRIMARY KEY,
    pid TEXT NOT NULL,
    out_trade_no TEXT NOT NULL,
    channel_id TEXT NOT NULL REFERENCES channels(id),
    method TEXT NOT NULL,
    name TEXT NOT NULL,
    amount_cent INTEGER NOT NULL CHECK (amount_cent > 0),
    param TEXT NOT NULL DEFAULT '',
    notify_url TEXT NOT NULL,
    return_url TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL CHECK (status IN ('pending', 'paid', 'expired', 'closed')),
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    paid_at TEXT,
    UNIQUE (pid, out_trade_no)
);

CREATE TABLE receipts (
    id INTEGER PRIMARY KEY,
    provider_id TEXT NOT NULL,
    account_id TEXT NOT NULL,
    transaction_id TEXT NOT NULL,
    order_reference TEXT NOT NULL,
    amount_cent INTEGER NOT NULL CHECK (amount_cent > 0),
    buyer TEXT NOT NULL DEFAULT '',
    occurred_at TEXT NOT NULL,
    matched_trade_no TEXT UNIQUE REFERENCES orders(trade_no),
    UNIQUE (provider_id, account_id, transaction_id)
);

CREATE TABLE notification_jobs (
    id INTEGER PRIMARY KEY,
    trade_no TEXT NOT NULL UNIQUE REFERENCES orders(trade_no),
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    next_attempt_at TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('pending', 'running', 'delivered', 'exhausted')),
    lease_until TEXT,
    last_error TEXT NOT NULL DEFAULT ''
);

CREATE TABLE poll_checkpoints (
    channel_id TEXT PRIMARY KEY REFERENCES channels(id),
    completed_until TEXT NOT NULL,
    window_until TEXT,
    page_token TEXT NOT NULL DEFAULT ''
);
