// 本文件实现 RFC 6238 的六位 TOTP；SHA-1 仅用于验证器兼容的 HMAC。
package admin

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"time"
)

// NewTOTPSecret 生成用于添加验证器的随机绑定密钥，随机数据长度为 160 位。
func NewTOTPSecret() (string, error) {
	raw := make([]byte, 20)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw), nil
}

// MatchTOTP 检查动态码是否匹配当前、前一个或后一个 30 秒周期。
// 返回匹配的周期编号，供调用方记录并阻止该周期再次用于登录。
func MatchTOTP(secret, code string, now time.Time) (int64, error) {
	if len(code) != 6 {
		return 0, ErrCredentials
	}
	for _, c := range code {
		if c < '0' || c > '9' {
			return 0, ErrCredentials
		}
	}
	raw, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil || len(raw) != 20 {
		return 0, errors.New("invalid TOTP seed")
	}
	step := now.Unix() / 30
	// 优先未来周期，极少数相邻周期验证码相同时仍严格防止二次消费。
	for _, offset := range []int64{1, 0, -1} {
		candidate := step + offset
		if candidate < 0 {
			continue
		}
		var counter [8]byte
		binary.BigEndian.PutUint64(counter[:], uint64(candidate))
		mac := hmac.New(sha1.New, raw)
		mac.Write(counter[:])
		sum := mac.Sum(nil)
		start := sum[len(sum)-1] & 15
		value := (binary.BigEndian.Uint32(sum[start:start+4]) & 0x7fffffff) % 1_000_000
		expected := fmt.Sprintf("%06d", value)
		if subtle.ConstantTimeCompare([]byte(expected), []byte(code)) == 1 {
			return candidate, nil
		}
	}
	return 0, ErrCredentials
}
