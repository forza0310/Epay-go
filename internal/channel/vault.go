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
	SaveChannel(context.Context, string, string, string, string, bool, []byte) error
	LoadChannel(context.Context, string) (string, string, string, bool, []byte, error)
	ListChannels(context.Context) ([]Channel, error)
	UpdateChannelMetadata(context.Context, Channel) (bool, error)
}

// Vault 将配置仓储、加密器和本次构建可用的插件绑定。
type Vault struct {
	repository ConfigRepository
	box        *securebox.Box
	plugins    map[string]bool
}

// NewVault 通过显式插件白名单拒绝未经编译注册的插件配置。
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

// Save 校验 JSON 对象并加密保存；更新留空保留旧配置，新增不得为空。
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

// Load 仅供可信内部调用解密配置；HTTP 管理页面不得调用并返回明文。
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

// List 只读取可展示的元数据，不读取或解密密钥。
func (v *Vault) List(ctx context.Context) ([]Channel, error) { return v.repository.ListChannels(ctx) }
func validLabel(value string) bool {
	return utf8.ValidString(value) && strings.TrimSpace(value) != "" && len(value) <= 200 && !strings.ContainsAny(value, "\r\n\x00")
}
