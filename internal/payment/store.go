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

// Store 将资金相关原子操作作为显式边界，禁止业务层分别提交状态和通知。
// Create 对相同 PID/out_trade_no 的相同业务请求返回已有订单；内容冲突返回 ErrConflict。
// Confirm 必须在同一事务内登记/关联唯一流水、确认待支付订单、创建唯一通知任务。
// 超时、已关闭、金额或账户不匹配的流水不得自动发货，转入异常处理。
type Store interface {
	Create(context.Context, Order) (Order, error)
	FindByTradeNo(context.Context, string) (Order, error)
	FindByOutTradeNo(context.Context, string, string) (Order, error)
	Confirm(context.Context, string, Receipt) (Confirmation, error)
}
