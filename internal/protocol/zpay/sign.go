// Package zpay 实现 ZPAY/易支付协议边界；MD5 仅用于既定接口兼容，不用于密码存储。
package zpay

import (
	"crypto/md5" // ZPAY 文档指定的兼容算法。
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net/url"
	"sort"
	"strings"
)

// Sign 按参数名 ASCII 顺序连接原始参数值；只排除 sign、sign_type 和空字符串。
// Values 必须来自单一参数源，重复参数一律拒绝，以消除签名解析歧义。
func Sign(values url.Values, key string) (string, error) {
	if key == "" {
		return "", errors.New("empty signing key")
	}
	names := make([]string, 0, len(values))
	for name, entries := range values {
		if name == "" || len(entries) != 1 {
			return "", errors.New("ambiguous signing parameters")
		}
		if name == "sign" || name == "sign_type" || entries[0] == "" {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	pairs := make([]string, 0, len(names))
	for _, name := range names {
		pairs = append(pairs, name+"="+values[name][0])
	}
	digest := md5.Sum([]byte(strings.Join(pairs, "&") + key))
	return hex.EncodeToString(digest[:]), nil
}

// Verify 只接受文档规定的 MD5 签名类型，并对固定长度的签名做恒定时间比较。
func Verify(values url.Values, key string) bool {
	if values.Get("sign_type") != "MD5" {
		return false
	}
	actual := values.Get("sign")
	if len(actual) != 32 {
		return false
	}
	expected, err := Sign(values, key)
	return err == nil && subtle.ConstantTimeCompare([]byte(actual), []byte(expected)) == 1
}
