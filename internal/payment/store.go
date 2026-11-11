// 本文件约定核心存储的原子性要求，具体 SQLite 实现待架构确认后接入。
package payment

import (
	"context"
	"errors"
)

var (
	ErrNotFound = errors.New("order not found")
	ErrConflict = errors.New("order or receipt conflicts with existing data")
)

// Confirmation 表示一次幂等确认的结果，Applied=false 表示已处理的同一笔入账。
type Confirmation struct {
	Applied bool
	Order   Order
}

// Store 约定订单保存、查询和到账确认方法；订单状态与通知任务必须在同一事务中保存。
type Store interface {
	// Create 保存订单；相同 PID 和外部订单号的相同请求返回已有订单，内容不一致时返回 ErrConflict。
	Create(context.Context, Order) (Order, error)
	// FindByTradeNo 按系统订单号查询订单。
	FindByTradeNo(context.Context, string) (Order, error)
	// FindByOutTradeNo 按 PID 和调用方提供的订单号查询订单。
	FindByOutTradeNo(context.Context, string, string) (Order, error)
	// Confirm 在同一事务中登记到账流水、确认订单已支付并创建通知任务；重复到账不得重复处理。
	// 超时、已关闭、金额或账户不匹配的订单不得自动确认，应交给异常处理流程。
	Confirm(context.Context, string, Receipt) (Confirmation, error)
}
