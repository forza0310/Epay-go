// Package channel 表示管理员配置的收款通道，一个插件可以配置多个账户。
package channel

// Channel 仅保存可展示的元数据；明文私钥不进入领域对象。
type Channel struct {
	ID        string
	PluginID  string
	AccountID string
	Name      string
	Enabled   bool
}
