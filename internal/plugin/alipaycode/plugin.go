// Package alipaycode 移植旧项目的支付宝账单匹配收款流程，目前仅定义接入边界。
package alipaycode

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"

	"epay/internal/payment"
	"epay/internal/plugin"
)

// Config 是此插件的运行配置；最终由加密存储提供，不作为公开 HTTP DTO。
type Config struct {
	AppID           string `json:"app_id"`
	PayeeUID        string `json:"payee_uid"`
	AppPrivateKey   string `json:"app_private_key"`
	AlipayPublicKey string `json:"alipay_public_key"`
	AppAuthToken    string `json:"app_auth_token,omitempty"`
	TransferMode    string `json:"transfer_mode"`
}

// Plugin 实现付款入口与账务采集契约；不依赖原 PHP 运行环境。
type Plugin struct{}

var _ plugin.Provider = (*Plugin)(nil)
var _ plugin.Poller = (*Plugin)(nil)

// New 创建不保存账户配置的插件实例；每次调用时单独传入配置，避免混用不同账户的信息。
func New() *Plugin { return &Plugin{} }

// Describe 返回插件名称和支持的支付方式，并标明支付功能尚未实现。
func (*Plugin) Describe() plugin.Descriptor {
	return plugin.Descriptor{ID: "alipaycode", Name: "支付宝账单收款", Methods: []string{"alipay"}, Polling: true, Implemented: false}
}

// ValidateConfig 检查配置的 JSON 格式、必填字段和转账模式，不验证密钥是否正确或支付宝是否能连接。
func (*Plugin) ValidateConfig(raw json.RawMessage) error {
	var cfg Config
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return errors.New("invalid alipaycode configuration")
	}
	if decoder.Decode(new(any)) != io.EOF {
		return errors.New("invalid alipaycode configuration")
	}
	if cfg.AppID == "" || cfg.PayeeUID == "" || cfg.AppPrivateKey == "" || cfg.AlipayPublicKey == "" {
		return errors.New("alipaycode requires app_id, payee_uid and key configuration")
	}
	if cfg.TransferMode != "normal" && cfg.TransferMode != "confirmation" {
		return errors.New("transfer_mode must be normal or confirmation")
	}
	return nil
}

// CreatePayment 预留支付宝付款入口；当前返回未实现错误，不会生成可用的付款请求。
func (*Plugin) CreatePayment(context.Context, json.RawMessage, plugin.Checkout) (payment.PaymentAction, error) {
	return payment.PaymentAction{}, plugin.ErrNotImplemented
}

// Poll 预留支付宝账单查询入口；当前返回未实现错误，不会调用支付宝。
func (*Plugin) Poll(context.Context, json.RawMessage, plugin.PollRequest) (plugin.PollResult, error) {
	return plugin.PollResult{}, plugin.ErrNotImplemented
}
