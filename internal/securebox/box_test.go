// 本文件验证通道配置的机密性和通道绑定，避免密文被跨配置复制。
package securebox

import "testing"

// TestBoxBindsCiphertextToChannel 验证配置可以正常解密，修改密文或将密文复制给其他通道后会解密失败。
func TestBoxBindsCiphertextToChannel(t *testing.T) {
	box, err := New([]byte("01234567890123456789012345678901"))
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, err := box.Seal("channel-a", []byte(`{"app_secret":"fixture"}`))
	if err != nil {
		t.Fatal(err)
	}
	plaintext, err := box.Open("channel-a", ciphertext)
	if err != nil || string(plaintext) != `{"app_secret":"fixture"}` {
		t.Fatalf("open = %q, %v", plaintext, err)
	}
	if _, err := box.Open("channel-b", ciphertext); err == nil {
		t.Fatal("ciphertext opened for another channel")
	}
	ciphertext[0] ^= 1
	if _, err := box.Open("channel-a", ciphertext); err == nil {
		t.Fatal("tampered ciphertext opened")
	}
}
