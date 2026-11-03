// Package payment 定义支付领域数据和持久化边界，不依赖 HTTP 或具体渠道。
package payment

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Money 是以分计的非负人民币金额；协议边界禁止通过浮点数转换。
type Money int64

var ErrInvalidMoney = errors.New("invalid money: expected a nonnegative decimal with at most two fractional digits")

// ParseMoney 解析协议金额，拒绝符号、指数、空格、精度丢失与溢出。
// 零金额可用于余额；创建支付订单必须另外要求金额大于零。
func ParseMoney(raw string) (Money, error) {
	parts := strings.Split(raw, ".")
	if len(parts) > 2 || len(parts[0]) == 0 {
		return 0, ErrInvalidMoney
	}
	for _, part := range parts {
		if part == "" {
			return 0, ErrInvalidMoney
		}
		for _, c := range part {
			if c < '0' || c > '9' {
				return 0, ErrInvalidMoney
			}
		}
	}
	fraction := "00"
	if len(parts) == 2 {
		if len(parts[1]) > 2 {
			return 0, ErrInvalidMoney
		}
		fraction = parts[1]
		if len(fraction) == 1 {
			fraction += "0"
		}
	}
	cents, err := strconv.ParseInt(parts[0]+fraction, 10, 64)
	if err != nil {
		return 0, ErrInvalidMoney
	}
	return Money(cents), nil
}

// String 返回固定两位小数，用于持久化记录展示和协议响应。
func (m Money) String() string { return fmt.Sprintf("%d.%02d", m/100, m%100) }
