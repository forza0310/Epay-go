// 本文件验证通道配置的机密性和通道绑定，避免密文被跨配置复制。
package securebox

import (
	"bytes"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

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

// TestKeyFileLifecycle 验证并发创建和重启始终使用同一个密钥；禁止补建时不会创建文件，损坏文件、不安全权限和符号链接均被拒绝。
func TestKeyFileLifecycle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data", "master.key")
	var wg sync.WaitGroup
	keys := make(chan []byte, 4)
	for range 4 {
		wg.Go(func() {
			key, err := LoadKeyFile(path, true)
			if err != nil {
				t.Error(err)
				return
			}
			keys <- key
		})
	}
	wg.Wait()
	close(keys)
	var first []byte
	for key := range keys {
		if first == nil {
			first = key
		}
		if len(key) != 32 || !bytes.Equal(key, first) {
			t.Fatal("concurrent creation changed key")
		}
	}
	key, err := LoadKeyFile(path, false)
	if err != nil || !bytes.Equal(key, first) {
		t.Fatal("restart did not reuse key", err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal("key permissions")
	}
	for _, contents := range []string{"", "invalid", string(bytes.Repeat([]byte("x"), 200))} {
		if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadKeyFile(path, true); err == nil {
			t.Fatal("invalid key silently replaced")
		}
		got, _ := os.ReadFile(path)
		if string(got) != contents {
			t.Fatal("invalid key overwritten")
		}
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadKeyFile(path, true); err == nil {
		t.Fatal("public key permissions accepted")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadKeyFile(path, false); err == nil {
		t.Fatal("missing original key recreated")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("missing key must stay missing")
	}
	target := filepath.Join(t.TempDir(), "target")
	if err := os.WriteFile(target, []byte("unchanged"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadKeyFile(path, true); err == nil {
		t.Fatal("symlink accepted")
	}
}
