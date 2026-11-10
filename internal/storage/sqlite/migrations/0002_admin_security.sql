-- 管理员双因素、主密钥校验及安全事件；保留第一版数据库的迁移路径。
ALTER TABLE owner ADD COLUMN totp_ciphertext BLOB;
ALTER TABLE owner ADD COLUMN last_totp_step INTEGER NOT NULL DEFAULT -1;
CREATE TABLE security_state (id INTEGER PRIMARY KEY CHECK (id = 1), key_check BLOB NOT NULL);
CREATE TABLE audit_events (
    id INTEGER PRIMARY KEY,
    event TEXT NOT NULL,
    subject TEXT NOT NULL DEFAULT '',
    occurred_at INTEGER NOT NULL
);
CREATE INDEX audit_events_time ON audit_events(occurred_at);
-- 密码单因素的历史会话在升级后全部撤销。
DELETE FROM admin_sessions;
