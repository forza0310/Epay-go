-- SQLite 数据模型草案，框架阶段不执行。正式接入时迁移版本与驱动一并确定。
-- 金额单位为分；时间统一存 UTC RFC3339；数据库只用于本地单实例部署。
PRAGMA foreign_keys = ON;

-- 唯一所有者，不引入商户、分账或平台余额表。鉴权实现时补充会话与恢复凭证迁移。
CREATE TABLE owner (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    password_hash TEXT NOT NULL,
    totp_secret_ciphertext BLOB,
    credential_version INTEGER NOT NULL DEFAULT 1
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

-- 保存未匹配到账，停机补查时也不会把同一流水用于第二张订单。
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

-- 与订单确认同一事务插入；发送采用持久化重试，不在数据库事务中进行网络请求。
CREATE TABLE notification_jobs (
    id INTEGER PRIMARY KEY,
    trade_no TEXT NOT NULL UNIQUE REFERENCES orders(trade_no),
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    next_attempt_at TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('pending', 'running', 'delivered', 'exhausted')),
    lease_until TEXT,
    last_error TEXT NOT NULL DEFAULT ''
);

-- 单个分页窗口固定 upper bound；完成落库后再更新检查点，允许重叠读取。
CREATE TABLE poll_checkpoints (
    channel_id TEXT PRIMARY KEY REFERENCES channels(id),
    completed_until TEXT NOT NULL,
    window_until TEXT,
    page_token TEXT NOT NULL DEFAULT ''
);

CREATE INDEX orders_pending_channel ON orders(channel_id, status, created_at);
CREATE INDEX notification_jobs_due ON notification_jobs(state, next_attempt_at);
