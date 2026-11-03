// Package plugin 定义可信支付插件契约及注册表，插件使用结构化输入输出。
package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"epay/internal/payment"
)

var ErrNotImplemented = errors.New("plugin capability is scaffolded but not implemented")

// Descriptor 描述插件能力，Implemented=false 时禁止用于实际下单。
type Descriptor struct {
	ID          string
	Name        string
	Methods     []string
	Polling     bool
	Implemented bool
}

// Checkout 是从核心传入的已鉴权、已持久化订单及可信站点地址。
type Checkout struct {
	Order         payment.Order
	PublicBaseURL string
}

// Provider 实现插件公共能力；运行配置由核心解密后按调用提供，禁止持久化到日志。
type Provider interface {
	Describe() Descriptor
	ValidateConfig(json.RawMessage) error
	CreatePayment(context.Context, json.RawMessage, Checkout) (payment.PaymentAction, error)
}

// PollRequest 支持持久化时间窗口与分页，不依赖进程内的“最近几分钟”。
type PollRequest struct {
	From      time.Time
	Until     time.Time
	PageToken string
}

// PollResult 由核心落库后再推进游标；分页结束不代表已成功持久化。
type PollResult struct {
	Receipts      []payment.Receipt
	NextPageToken string
}

// Poller 是账单型插件的可选能力；核心统一负责调度、重试和去重。
type Poller interface {
	Poll(context.Context, json.RawMessage, PollRequest) (PollResult, error)
}
