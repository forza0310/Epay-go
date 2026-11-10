// 本文件保存通道密文，并只向管理页面提供非敏感元数据。
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"epay/internal/channel"
)

// SaveChannel 事务保存通道元数据、密文和不含凭证的审计记录。
func (s *Store) SaveChannel(ctx context.Context, id, pluginID, accountID, name string, enabled bool, ciphertext []byte) error {
	active := 0
	if enabled {
		active = 1
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO channels(id, plugin_id, account_id, name, enabled, config_ciphertext)
		VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT(id) DO UPDATE SET plugin_id = excluded.plugin_id,
		account_id = excluded.account_id, name = excluded.name, enabled = excluded.enabled,
		config_ciphertext = excluded.config_ciphertext`, id, pluginID, accountID, name, active, ciphertext)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO audit_events(event,subject,occurred_at) VALUES('channel_saved',?,?)`, id, time.Now().Unix()); err != nil {
		return err
	}
	return tx.Commit()
}

// LoadChannel 读取密文，不在持久化层解密配置。
func (s *Store) LoadChannel(ctx context.Context, id string) (pluginID, accountID, name string, enabled bool, ciphertext []byte, err error) {
	var active int
	err = s.db.QueryRowContext(ctx, `SELECT plugin_id, account_id, name, enabled, config_ciphertext FROM channels WHERE id = ?`, id).
		Scan(&pluginID, &accountID, &name, &active, &ciphertext)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return pluginID, accountID, name, active == 1, ciphertext, err
}

// ListChannels 返回列表元数据，不读取配置密文。
func (s *Store) ListChannels(ctx context.Context) ([]channel.Channel, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,plugin_id,account_id,name,enabled FROM channels ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []channel.Channel{}
	for rows.Next() {
		var c channel.Channel
		if err := rows.Scan(&c.ID, &c.PluginID, &c.AccountID, &c.Name, &c.Enabled); err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, rows.Err()
}

// UpdateChannelMetadata 原子保留密文；账户或插件变化时要求重新提交完整配置。
func (s *Store) UpdateChannelMetadata(ctx context.Context, c channel.Channel) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE channels SET name=?,enabled=? WHERE id=? AND plugin_id=? AND account_id=?`, c.Name, c.Enabled, c.ID, c.PluginID, c.AccountID)
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
	if _, err = tx.ExecContext(ctx, `INSERT INTO audit_events(event,subject,occurred_at) VALUES('channel_updated',?,?)`, c.ID, time.Now().Unix()); err != nil {
		return false, err
	}
	return true, tx.Commit()
}
