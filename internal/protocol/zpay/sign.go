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

// Sign 按参数名字典顺序拼接原始参数值，追加密钥后计算 MD5 签名；跳过 sign、sign_type 和空值。
// 参数必须来自同一个来源；参数名为空或同一参数出现多次时返回错误，避免双方使用不同的值验签。
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

// Verify 检查签名类型是否为 MD5，并重新计算签名与传入值比较；参数或签名不合法时返回 false。
// 使用恒定时间比较，避免通过比较耗时泄露匹配位置。
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
