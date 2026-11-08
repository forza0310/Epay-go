// 本文件定义兼容层响应与通知字段，避免外部命名污染内部领域模型。
package zpay

// ErrorResponse 保留文档示例中 code="error" 的失败格式。
type ErrorResponse struct {
	Code    string `json:"code"` // `json:"code"` 是结构体标签，他是一种元数据。encoding/json 会读取它，决定 JSON 字段名。
	Message string `json:"msg"`
	// 标准库 JSON 编码通常只处理导出字段，也就是首字母大写的字段。
}

// PayResponse 保留 O_id 大小写；本系统的 O_id 将使用自身订单号。
// payurl2 为微信能力字段，首个支付宝插件不返回；不对接 ZPAY 专属小程序。
type PayResponse struct {
	Code    int    `json:"code"`
	Message string `json:"msg"`
	OrderID string `json:"O_id"`
	TradeNo string `json:"trade_no"`
	PayURL  string `json:"payurl,omitempty"`
	PayURL2 string `json:"payurl2,omitempty"`
	QRCode  string `json:"qrcode,omitempty"`
	Image   string `json:"img,omitempty"`
}

// OrderResponse 中 PID 使用字符串，以支持文档定义的字母数字标识及大整数 ID。
type OrderResponse struct {
	Code       int    `json:"code"`
	Message    string `json:"msg"`
	TradeNo    string `json:"trade_no"`
	OutTradeNo string `json:"out_trade_no"`
	Method     string `json:"type"`
	PID        string `json:"pid"`
	CreatedAt  string `json:"addtime"`
	PaidAt     string `json:"endtime"`
	Name       string `json:"name"`
	Money      string `json:"money"`
	Status     int    `json:"status"`
	Param      string `json:"param"`
	Buyer      string `json:"buyer"`
}

// BalanceResponse 的业务含义尚待确认，不将累计收款金额冒充可用余额。
type BalanceResponse struct {
	Code    int    `json:"code"`
	Message string `json:"msg"`
	Balance string `json:"balance"`
}
