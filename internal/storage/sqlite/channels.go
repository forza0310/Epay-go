// 本文件保存通道密文，并只向管理页面提供非敏感元数据。
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"epay/internal/channel"
)

// SaveChannel 在同一事务中新增或更新通道信息、配置密文并记录保存事件；审计记录不包含配置值。
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

// LoadChannel 读取指定通道的信息和配置密文，不在数据库层解密；通道不存在时返回 ErrNotFound。
func (s *Store) LoadChannel(ctx context.Context, id string) (pluginID, accountID, name string, enabled bool, ciphertext []byte, err error) {
	var active int
	err = s.db.QueryRowContext(ctx, `SELECT plugin_id, account_id, name, enabled, config_ciphertext FROM channels WHERE id = ?`, id).
		Scan(&pluginID, &accountID, &name, &active, &ciphertext)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return pluginID, accountID, name, active == 1, ciphertext, err
}

// ListChannels 读取按 ID 排序的通道列表，只返回名称、账户和启用状态等信息，不读取配置密文。
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

// UpdateChannelMetadata 只更新通道名称和启用状态，保留原配置密文，并在同一事务中记录修改事件。
// 通道不存在，或账户、插件与已保存的信息不一致时，不执行更新并返回 false。
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
