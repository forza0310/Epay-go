// 本文件持久化管理员凭证、限流和会话；动态码消费与会话创建在同一事务中。
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"epay/internal/admin"
)

// Credentials 读取唯一管理员；不存在与存储错误分别返回。
func (s *Store) Credentials(ctx context.Context) (admin.Credentials, bool, error) {
	var c admin.Credentials
	err := s.db.QueryRowContext(ctx, `SELECT password_hash, totp_ciphertext, last_totp_step FROM owner WHERE id=1`).Scan(&c.PasswordHash, &c.TOTPCiphertext, &c.LastTOTPStep)
	if errors.Is(err, sql.ErrNoRows) {
		return c, false, nil
	}
	return c, err == nil, err
}

// CreateAdministrator 只初始化一次，记录初始化审计事件。
func (s *Store) CreateAdministrator(ctx context.Context, c admin.Credentials, now time.Time) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `INSERT INTO owner(id,password_hash,totp_ciphertext,last_totp_step,created_at) VALUES(1,?,?,?,?) ON CONFLICT(id) DO NOTHING`, c.PasswordHash, c.TOTPCiphertext, c.LastTOTPStep, now.Unix())
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if n == 0 {
		return false, nil
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO audit_events(event,occurred_at) VALUES('admin_initialized',?)`, now.Unix()); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

// ResetTOTP 替换种子时验证凭证未变更，并原子撤销所有旧会话。
func (s *Store) ResetTOTP(ctx context.Context, expected admin.Credentials, ciphertext []byte, step int64, now time.Time) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE owner SET totp_ciphertext=?,last_totp_step=? WHERE id=1 AND password_hash=? AND totp_ciphertext IS ?`, ciphertext, step, expected.PasswordHash, expected.TOTPCiphertext)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if n != 1 {
		return false, nil
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM admin_sessions`); err != nil {
		return false, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO audit_events(event,occurred_at) VALUES('totp_reset',?)`, now.Unix()); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

// CommitLogin 以条件更新消费动态码；重放、并发登录和种子重置竞态均不能绕过校验。
func (s *Store) CommitLogin(ctx context.Context, c admin.Credentials, step int64, tokenHash, csrfHash []byte, now, expires time.Time, remote string) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE owner SET last_totp_step=? WHERE id=1 AND password_hash=? AND totp_ciphertext=? AND last_totp_step<?`, step, c.PasswordHash, c.TOTPCiphertext, step)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if n != 1 {
		return false, nil
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO admin_sessions(token_hash,csrf_hash,created_at,expires_at) VALUES(?,?,?,?)`, tokenHash, csrfHash, now.Unix(), expires.Unix()); err != nil {
		return false, err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM admin_login_attempts WHERE remote_key=?`, remote); err != nil {
		return false, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO audit_events(event,occurred_at) VALUES('admin_login',?)`, now.Unix()); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

// ReserveLoginAttempt 持久化来源与账户限流，并返回剩余等待时间；不信任代理头。
func (s *Store) ReserveLoginAttempt(ctx context.Context, remote string, now time.Time) (bool, time.Duration, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, 0, err
	}
	defer tx.Rollback()
	stamp := now.Unix()
	if _, err = tx.ExecContext(ctx, `DELETE FROM admin_sessions WHERE expires_at<=?`, stamp); err != nil {
		return false, 0, err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM admin_login_attempts WHERE window_started<=? AND locked_until<=?`, now.Add(-loginWindow).Unix(), stamp); err != nil {
		return false, 0, err
	}
	for _, limit := range []struct {
		key string
		max int
	}{{"global", 20}, {remote, loginLimit}} {
		var attempts int
		var started, locked int64
		err = tx.QueryRowContext(ctx, `SELECT attempts,window_started,locked_until FROM admin_login_attempts WHERE remote_key=?`, limit.key).Scan(&attempts, &started, &locked)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return false, 0, err
		}
		if locked > stamp {
			return false, time.Unix(locked, 0).Sub(now), tx.Commit()
		}
		if errors.Is(err, sql.ErrNoRows) || stamp-started >= int64(loginWindow.Seconds()) {
			attempts, started, locked = 0, stamp, 0
		}
		if attempts >= limit.max {
			if _, err = tx.ExecContext(ctx, `UPDATE admin_login_attempts SET locked_until=? WHERE remote_key=?`, stamp+int64(loginLock.Seconds()), limit.key); err != nil {
				return false, 0, err
			}
			return false, time.Unix(stamp+int64(loginLock.Seconds()), 0).Sub(now), tx.Commit()
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO admin_login_attempts(remote_key,attempts,window_started,locked_until) VALUES(?,?,?,?) ON CONFLICT(remote_key) DO UPDATE SET attempts=excluded.attempts,window_started=excluded.window_started,locked_until=excluded.locked_until`, limit.key, attempts+1, started, locked); err != nil {
			return false, 0, err
		}
	}
	return true, 0, tx.Commit()
}

// SessionCSRFHash 只返回尚未过期且未撤销的会话凭证。
func (s *Store) SessionCSRFHash(ctx context.Context, tokenHash []byte, now time.Time) ([]byte, bool, error) {
	var hash []byte
	err := s.db.QueryRowContext(ctx, `SELECT csrf_hash FROM admin_sessions WHERE token_hash=? AND expires_at>?`, tokenHash, now.Unix()).Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	return hash, err == nil, err
}

// DeleteSession 在服务端撤销会话。
func (s *Store) DeleteSession(ctx context.Context, hash []byte) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `DELETE FROM admin_sessions WHERE token_hash=?`, hash); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO audit_events(event,occurred_at) VALUES('admin_logout',?)`, time.Now().Unix()); err != nil {
		return err
	}
	return tx.Commit()
}
