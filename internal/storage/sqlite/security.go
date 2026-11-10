// 本文件在数据库与部署主密钥之间建立可验证绑定，避免错误密钥静默启动。

package sqlite

import (
	"context"
	"errors"

	"epay/internal/securebox"
)

// VerifyKey 首次记录认证密文，后续每次启动必须能解密它。
func (s *Store) VerifyKey(ctx context.Context, box *securebox.Box) error {
	value, err := box.Seal("system:master-key", []byte("epay-key-check-v1"))
	if err != nil {
		return err
	}
	// 首次使用时保存密文
	if _, err = s.db.ExecContext(ctx, `INSERT INTO security_state(id,key_check) VALUES(1,?) ON CONFLICT(id) DO NOTHING`, value); err != nil {
		return err
	}
	// 读取数据库里的密文
	var ciphertext []byte
	if err = s.db.QueryRowContext(ctx, `SELECT key_check FROM security_state WHERE id=1`).Scan(&ciphertext); err != nil {
		return err
	}
	// 校验主密钥
	plain, err := box.Open("system:master-key", ciphertext)
	if err != nil || string(plain) != "epay-key-check-v1" {
		return errors.New("master key does not match database")
	}
	return nil
}
