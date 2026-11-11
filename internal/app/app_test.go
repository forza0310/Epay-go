// 本文件验证真实启动装配的密钥持久性与失败边界，只使用临时工作目录。
package app

import (
	"bytes"
	"encoding/base64"
	"io"
	"log/slog"
	"os"
	"testing"

	"epay/internal/config"
)

// TestStartupKeyPersistence 验证首次启动会生成密钥、重启会复用原密钥；已有数据库时，密钥丢失或不匹配会拒绝启动且不会生成替代密钥。
func TestStartupKeyPersistence(t *testing.T) {
	t.Chdir(t.TempDir())
	cfg := config.Config{Addr: "127.0.0.1:8080", DatabasePath: "data/epay.db"}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	start := func() {
		t.Helper()
		application, err := New(cfg, logger)
		if err != nil {
			t.Fatal(err)
		}
		if err := application.store.Close(); err != nil {
			t.Fatal(err)
		}
	}
	start()
	original, err := os.ReadFile("data/master.key")
	if err != nil {
		t.Fatal(err)
	}
	start()
	again, _ := os.ReadFile("data/master.key")
	if !bytes.Equal(original, again) {
		t.Fatal("restart changed the key")
	}
	if err := os.Remove("data/master.key"); err != nil {
		t.Fatal(err)
	}
	if a, err := New(cfg, logger); err == nil {
		a.store.Close()
		t.Fatal("missing key accepted")
	}
	if _, err := os.Stat("data/master.key"); !os.IsNotExist(err) {
		t.Fatal("replacement key created")
	}
	wrong := []byte(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32)))
	if err := os.WriteFile("data/master.key", wrong, 0600); err != nil {
		t.Fatal(err)
	}
	if a, err := New(cfg, logger); err == nil {
		a.store.Close()
		t.Fatal("wrong key accepted")
	}
	again, _ = os.ReadFile("data/master.key")
	if !bytes.Equal(wrong, again) {
		t.Fatal("wrong key overwritten")
	}
	if err := os.WriteFile("data/master.key", original, 0600); err != nil {
		t.Fatal(err)
	}
	start()
}
