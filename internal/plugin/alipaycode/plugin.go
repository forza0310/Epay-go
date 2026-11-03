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

// New 构造无状态插件，账户配置按调用传入，避免不同账户配置串用。
func New() *Plugin { return &Plugin{} }

// Describe 明确标记骨架状态，已注册不等于已经可以收款。
func (*Plugin) Describe() plugin.Descriptor {
	return plugin.Descriptor{ID: "alipaycode", Name: "支付宝账单收款", Methods: []string{"alipay"}, Polling: true, Implemented: false}
}

// ValidateConfig 仅校验配置结构；密钥格式、证书模式与联通性留待正式适配器实现。
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

// CreatePayment 将生成绑定金额和订单备注的入口，当前明确拒绝真实支付。
func (*Plugin) CreatePayment(context.Context, json.RawMessage, plugin.Checkout) (payment.PaymentAction, error) {
	return payment.PaymentAction{}, plugin.ErrNotImplemented
}

// Poll 将封装 alipay.data.bill.accountlog.query，并验证支付宝响应签名。
func (*Plugin) Poll(context.Context, json.RawMessage, plugin.PollRequest) (plugin.PollResult, error) {
	return plugin.PollResult{}, plugin.ErrNotImplemented
}
