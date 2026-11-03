// 本文件定义订单、到账流水和付款动作，内部状态与兼容协议状态分离。
package payment

import "time"

// Status 表示内部订单生命周期；兼容查询只将 Paid 映射为 status=1。
type Status string

const (
	Pending Status = "pending"
	Paid    Status = "paid"
	Expired Status = "expired"
	Closed  Status = "closed"
)

// Order 属于唯一收款管理员；PID 是协议凭证标识，不是多租户商户 ID。
type Order struct {
	TradeNo    string
	OutTradeNo string
	PID        string
	ChannelID  string
	Method     string
	Name       string
	Amount     Money
	Param      string
	NotifyURL  string
	ReturnURL  string
	Status     Status
	CreatedAt  time.Time
	ExpiresAt  time.Time
	PaidAt     *time.Time
}

// Receipt 是渠道返回并通过验证的入账候选；核心还需核对账户、金额和订单。
// ProviderID + AccountID + TransactionID 唯一，不以通道配置 ID 去重。
type Receipt struct {
	ProviderID     string
	AccountID      string
	TransactionID  string
	OrderReference string
	Amount         Money
	Incoming       bool
	Successful     bool
	Buyer          string
	OccurredAt     time.Time
}

// PaymentAction 返回结构化付款入口，插件不能向核心注入任意 HTML。
type PaymentAction struct {
	PayURL    string
	QRCode    string
	ImageURL  string
	ExpiresAt time.Time
}
