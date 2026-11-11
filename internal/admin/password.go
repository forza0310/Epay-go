// 本文件封装密码策略与 PBKDF2 摘要，不在日志或数据库保留明文密码。
package admin

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	passwordMinBytes   = 10
	passwordMaxBytes   = 1024
	passwordIterations = 600_000
	passwordSaltBytes  = 16
	passwordKeyBytes   = 32
)

// ErrWeakPassword 表示密码不符合所有认证入口共用的格式要求。
var ErrWeakPassword = fmt.Errorf("password must be %d-%d UTF-8 bytes", passwordMinBytes, passwordMaxBytes)

// ValidatePassword 检查密码是否为有效的 UTF-8 文本，且长度在 10–1024 字节之间。
func ValidatePassword(password string) error {
	if len(password) < passwordMinBytes || len(password) > passwordMaxBytes || !utf8.ValidString(password) {
		return ErrWeakPassword
	}
	return nil
}

// hashPassword 为密码生成带随机盐的 PBKDF2 哈希，返回值包含验证密码所需的算法参数和盐值。
func hashPassword(password string) (string, error) {
	salt := make([]byte, passwordSaltBytes)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}
	derived, err := pbkdf2.Key(sha256.New, password, salt, passwordIterations, passwordKeyBytes)
	if err != nil {
		return "", fmt.Errorf("derive password hash: %w", err)
	}
	return fmt.Sprintf("pbkdf2-sha256$%d$%s$%s", passwordIterations,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(derived)), nil
}

// verifyPassword 检查输入密码是否与保存的哈希匹配；密码或哈希格式不合法时返回 false。
// 比较哈希时使用恒定时间比较，避免通过比较耗时泄露匹配位置。
func verifyPassword(encoded, password string) bool {
	if ValidatePassword(password) != nil {
		return false
	}
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" || parts[1] != fmt.Sprint(passwordIterations) {
		return false
	}
	salt, err1 := base64.RawStdEncoding.DecodeString(parts[2])
	expected, err2 := base64.RawStdEncoding.DecodeString(parts[3])
	if err1 != nil || err2 != nil || len(salt) != passwordSaltBytes || len(expected) != passwordKeyBytes {
		return false
	}
	actual, err := pbkdf2.Key(sha256.New, password, salt, passwordIterations, passwordKeyBytes)
	return err == nil && subtle.ConstantTimeCompare(expected, actual) == 1
}
