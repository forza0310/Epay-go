// 本文件验证独立固定向量、篡改和重复参数，保护外部签名协议边界。
package zpay

import (
	"net/url"
	"testing"
)

// TestSignatureContract 用已知签名结果检查 ZPAY 协议兼容性，并验证修改参数、重复参数或无效签名设置会被拒绝。
func TestSignatureContract(t *testing.T) {
	// 固定向量由独立 Python hashlib 校验；中文、空格、&、+ 不进行二次编码，0 不被排除。
	values := url.Values{"b": {"中文 空格&+"}, "a": {"0"}, "empty": {""}, "sign_type": {"MD5"}}
	const expected = "abf504bb4772b1c2af4276cedb1fb0fa"
	got, err := Sign(values, "fixture-key")
	if err != nil || got != expected {
		t.Fatalf("signature = %q, %v", got, err)
	}
	values.Set("sign", expected)
	if !Verify(values, "fixture-key") {
		t.Fatal("valid signature rejected")
	}
	values.Set("a", "1")
	if Verify(values, "fixture-key") {
		t.Fatal("tampered amount accepted")
	}
	values.Set("a", "0")
	values.Set("sign_type", "RSA")
	if Verify(values, "fixture-key") {
		t.Fatal("unsupported signature type accepted")
	}
	values.Set("sign_type", "MD5")
	values.Add("a", "0")
	if _, err := Sign(values, "fixture-key"); err == nil {
		t.Fatal("duplicate input accepted")
	}
	if Verify(values, "fixture-key") {
		t.Fatal("ambiguous signature accepted")
	}
	values.Del("a")
	values.Add("sign", expected)
	if Verify(values, "fixture-key") {
		t.Fatal("duplicate signature accepted")
	}
	if _, err := Sign(url.Values{"a": {"1"}}, ""); err == nil {
		t.Fatal("empty key accepted")
	}
}
