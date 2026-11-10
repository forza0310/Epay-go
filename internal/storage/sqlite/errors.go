// 本文件集中定义 SQLite 适配器返回的稳定领域错误。
package sqlite

import "errors"

var (
	ErrNotFound = errors.New("record not found")
)
