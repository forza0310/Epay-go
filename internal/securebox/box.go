// Package securebox 使用 AES-GCM 加密需持久化的通道凭证与管理员 TOTP 种子。
package securebox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
)

// Box 是使用部署外置 256-bit 主密钥创建的不可变加密器。
type Box struct{ aead cipher.AEAD }

// New 拒绝非 32 字节密钥。
func New(key []byte) (*Box, error) {
	if len(key) != 32 {
		return nil, errors.New("master key must be exactly 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("initialize AES: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("initialize AES-GCM: %w", err)
	}
	return &Box{aead: aead}, nil
}

// Seal 使用随机 nonce，并将密文绑定到调用方指定的用途及对象标识。
func (b *Box) Seal(purpose string, plaintext []byte) ([]byte, error) {
	if purpose == "" {
		return nil, errors.New("encryption purpose is required")
	}
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generate encryption nonce: %w", err)
	}
	sealed := b.aead.Seal(nil, nonce, plaintext, []byte(purpose))
	return append(nonce, sealed...), nil
}

// Open 验证密文完整性和用途绑定；任何不匹配均返回错误。
func (b *Box) Open(purpose string, ciphertext []byte) ([]byte, error) {
	if purpose == "" || len(ciphertext) < b.aead.NonceSize()+b.aead.Overhead() {
		return nil, errors.New("invalid encrypted value")
	}
	nonce, sealed := ciphertext[:b.aead.NonceSize()], ciphertext[b.aead.NonceSize():]
	plaintext, err := b.aead.Open(nil, nonce, sealed, []byte(purpose))
	if err != nil {
		return nil, errors.New("encrypted value authentication failed")
	}
	return plaintext, nil
}
