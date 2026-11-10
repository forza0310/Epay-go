// 本文件定义管理员凭证的持久化数据结构，供身份服务与存储适配器共用。
package admin

// Credentials 是持久化凭证快照，不包含明文密码或 TOTP 种子。
type Credentials struct {
	PasswordHash   string
	TOTPCiphertext []byte
	LastTOTPStep   int64
}
