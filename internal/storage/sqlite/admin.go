// 本文件持久化管理员凭证、限流和会话；动态码消费与会话创建在同一事务中。
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"epay/internal/admin"
)

// Credentials 读取管理员的密码哈希、加密的 TOTP 绑定密钥和已用动态码周期，并返回管理员是否存在。
func (s *Store) Credentials(ctx context.Context) (admin.Credentials, bool, error) {
	var c admin.Credentials
	err := s.db.QueryRowContext(ctx, `SELECT password_hash, totp_ciphertext, last_totp_step FROM owner WHERE id=1`).Scan(&c.PasswordHash, &c.TOTPCiphertext, &c.LastTOTPStep)
	if errors.Is(err, sql.ErrNoRows) {
		return c, false, nil
	}
	return c, err == nil, err
}

// CreateAdministrator 在同一事务中创建管理员并记录初始化事件；管理员已存在时不覆盖，返回 false。
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

// ResetTOTP 在同一事务中更新验证器绑定、删除旧登录会话并记录重置事件。
// 如果密码或旧绑定已被其他操作修改，则不执行更新。
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

// CommitLogin 在同一事务中记录动态码已使用、保存登录会话、清除当前来源的尝试计数并记录登录事件。
// 如果密码或验证器绑定已变化，或该动态码周期已被使用，则拒绝创建会话。
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

// ReserveLoginAttempt 记录本次登录尝试，并检查当前来源和全账户的尝试次数。
// 达到限制时拒绝登录并返回还需等待的时长；计数和限制在重启后仍保留。
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

// SessionCSRFHash 查询指定会话的 CSRF 令牌哈希；会话不存在或已过期时返回未找到。
func (s *Store) SessionCSRFHash(ctx context.Context, tokenHash []byte, now time.Time) ([]byte, bool, error) {
	var hash []byte
	err := s.db.QueryRowContext(ctx, `SELECT csrf_hash FROM admin_sessions WHERE token_hash=? AND expires_at>?`, tokenHash, now.Unix()).Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	return hash, err == nil, err
}

// DeleteSession 在同一事务中删除指定登录会话并记录退出事件，使旧登录令牌失效。
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
