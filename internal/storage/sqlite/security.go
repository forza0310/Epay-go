// 本文件在数据库与部署主密钥之间建立可验证绑定，避免错误密钥静默启动。

package sqlite

import (
	"context"
	"errors"

	"epay/internal/securebox"
)

// VerifyKey 首次保存用当前主密钥加密的校验文本，后续启动时用主密钥解密并核对该文本。
// 解密失败或内容不符时返回错误，已有校验密文不会被覆盖。
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

// RequiresKey 检查数据库是否已有主密钥校验记录、管理员或通道。
// 存在任意一种记录时，调用方必须读取原主密钥，不能自动生成替代密钥。
func (s *Store) RequiresKey(ctx context.Context) (bool, error) {
	var required bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM security_state) OR EXISTS(SELECT 1 FROM owner) OR EXISTS(SELECT 1 FROM channels)`).Scan(&required)
	return required, err
}
