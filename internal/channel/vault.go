// 本文件在领域边界校验配置，并仅将认证加密后的插件凭证交给仓储。
package channel

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"

	"epay/internal/securebox"
)

// ErrInvalidConfig 表示可安全提示给管理员的通用输入错误。
var ErrInvalidConfig = errors.New("invalid channel configuration")
var channelIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

// ConfigRepository 只保存元数据与密文，不接收明文凭证。
type ConfigRepository interface {
	// SaveChannel 新增或更新通道信息及配置密文，并记录保存事件。
	SaveChannel(context.Context, string, string, string, string, bool, []byte) error
	// LoadChannel 读取指定通道的信息和配置密文，不负责解密。
	LoadChannel(context.Context, string) (string, string, string, bool, []byte, error)
	// ListChannels 读取通道列表，不返回配置密文。
	ListChannels(context.Context) ([]Channel, error)
	// UpdateChannelMetadata 保留配置密文，只修改名称和启用状态；账户或插件不匹配时返回 false。
	UpdateChannelMetadata(context.Context, Channel) (bool, error)
}

// Vault 将配置仓储、加密器和本次构建可用的插件绑定。
type Vault struct {
	repository ConfigRepository
	box        *securebox.Box
	plugins    map[string]bool
}

// NewVault 创建收款配置管理服务，只允许保存 pluginIDs 中列出的插件配置。
func NewVault(repository ConfigRepository, box *securebox.Box, pluginIDs []string) (*Vault, error) {
	if repository == nil || box == nil {
		return nil, errors.New("channel repository and encryption box are required")
	}
	allowed := make(map[string]bool, len(pluginIDs))
	for _, id := range pluginIDs {
		allowed[id] = true
	}
	return &Vault{repository, box, allowed}, nil
}

// Save 检查通道信息和 JSON 配置，将配置加密后保存。
// 编辑时配置留空会保留原配置，但新增通道或更换账户、插件时必须填写配置。
func (v *Vault) Save(ctx context.Context, c Channel, plain []byte) error {
	if !channelIDPattern.MatchString(c.ID) || !v.plugins[c.PluginID] || !validLabel(c.AccountID) || !validLabel(c.Name) {
		return ErrInvalidConfig
	}
	var ciphertext []byte
	if len(plain) == 0 {
		updated, err := v.repository.UpdateChannelMetadata(ctx, c)
		if err != nil {
			return err
		}
		if !updated {
			return ErrInvalidConfig
		}
		return nil
	} else {
		var object map[string]json.RawMessage
		if len(plain) > 32<<10 || !utf8.Valid(plain) || json.Unmarshal(plain, &object) != nil || len(object) == 0 {
			return ErrInvalidConfig
		}
		var err error
		ciphertext, err = v.box.Seal("channel:"+c.ID, plain)
		if err != nil {
			return err
		}
	}
	return v.repository.SaveChannel(ctx, c.ID, c.PluginID, c.AccountID, c.Name, c.Enabled, ciphertext)
}

// Load 读取并解密指定通道的配置，供服务端调用插件时使用；不得将返回的明文发送到管理页面。
func (v *Vault) Load(ctx context.Context, id string) (Channel, []byte, error) {
	plugin, account, name, enabled, ciphertext, err := v.repository.LoadChannel(ctx, id)
	if err != nil {
		return Channel{}, nil, err
	}
	plain, err := v.box.Open("channel:"+id, ciphertext)
	if err != nil {
		return Channel{}, nil, err
	}
	return Channel{ID: id, PluginID: plugin, AccountID: account, Name: name, Enabled: enabled}, plain, nil
}

// List 读取通道名称、账户和启用状态等列表信息，不读取配置密文。
func (v *Vault) List(ctx context.Context) ([]Channel, error) { return v.repository.ListChannels(ctx) }

// validLabel 检查名称或账户标识是否为非空 UTF-8 文本，长度不超过 200 字节，且不含换行或空字符。
func validLabel(value string) bool {
	return utf8.ValidString(value) && strings.TrimSpace(value) != "" && len(value) <= 200 && !strings.ContainsAny(value, "\r\n\x00")
}
