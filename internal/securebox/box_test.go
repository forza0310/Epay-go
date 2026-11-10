// 本文件验证通道配置的机密性和通道绑定，避免密文被跨配置复制。
package securebox

import "testing"

// TestBoxBindsCiphertextToChannel 验证配置可正确解密，并拒绝跨通道复制和篡改后的密文。
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
