// Package channel 表示管理员配置的收款通道，一个插件可以配置多个账户。
package channel

// Channel 仅保存密钥引用；明文私钥不进入领域对象、日志或 API 返回值。
type Channel struct {
	ID        string
	PluginID  string
	AccountID string
	Name      string
	Enabled   bool
	SecretRef string
}
