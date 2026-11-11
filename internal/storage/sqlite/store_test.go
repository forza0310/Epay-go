// 本文件验证迁移、防重放事务、限流及加密配置的重启与完整性约束。
package sqlite_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"epay/internal/channel"
	"epay/internal/securebox"
	"epay/internal/storage/sqlite"
)

// TestEncryptedPersistenceAndMigrations 验证重启后数据库结构和加密配置仍可使用，错误主密钥、复制到其他通道的密文及未知迁移版本会被拒绝。
func TestEncryptedPersistenceAndMigrations(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "data", "epay.db")
	store, err := sqlite.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { store.Close() }()
	box, _ := securebox.New([]byte("01234567890123456789012345678901"))
	if err = store.VerifyKey(ctx, box); err != nil {
		t.Fatal(err)
	}
	vault, _ := channel.NewVault(store, box, []string{"alipaycode"})
	c := channel.Channel{ID: "alipay-1", PluginID: "alipaycode", AccountID: "2088", Name: "主账户", Enabled: true}
	plain := []byte(`{"private_key":"fixture-private-key"}`)
	if err = vault.Save(ctx, c, plain); err != nil {
		t.Fatal(err)
	}
	c.Enabled = false
	c.Name = "停用账户"
	if err = vault.Save(ctx, c, nil); err != nil {
		t.Fatal(err)
	}
	c.AccountID = "other"
	if !errors.Is(vault.Save(ctx, c, nil), channel.ErrInvalidConfig) {
		t.Fatal("account changed without new config")
	}
	c.AccountID = "2088"
	for _, input := range []string{`[]`, `null`, `{}`, `broken`} {
		if !errors.Is(vault.Save(ctx, c, []byte(input)), channel.ErrInvalidConfig) {
			t.Fatalf("invalid JSON accepted: %s", input)
		}
	}
	_, _, _, _, encrypted, err := store.LoadChannel(ctx, c.ID)
	if err != nil || bytes.Contains(encrypted, plain) {
		t.Fatal("plaintext leaked to storage")
	}
	store.Close()
	store, err = sqlite.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	vault, _ = channel.NewVault(store, box, []string{"alipaycode"})
	loaded, decoded, err := vault.Load(ctx, c.ID)
	if err != nil || loaded.Enabled || !bytes.Equal(decoded, plain) {
		t.Fatalf("restart: %+v %v", loaded, err)
	}
	wrong, _ := securebox.New([]byte("abcdefghijklmnopqrstuvwxyz123456"))
	if store.VerifyKey(ctx, wrong) == nil {
		t.Fatal("wrong master key accepted")
	}
	if _, _, err = vault.Load(ctx, "missing"); !errors.Is(err, sqlite.ErrNotFound) {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatalf("database mode: %v", info.Mode())
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var versions int
	if err = db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&versions); err != nil || versions != 2 {
		t.Fatalf("migrations: %d %v", versions, err)
	}
	if _, err = db.Exec(`INSERT INTO channels SELECT 'copied',plugin_id,account_id,name,enabled,config_ciphertext FROM channels WHERE id='alipay-1'`); err != nil {
		t.Fatal(err)
	}
	if _, _, err = vault.Load(ctx, "copied"); err == nil {
		t.Fatal("cross-channel ciphertext accepted")
	}
	if _, err = db.Exec(`INSERT INTO schema_migrations(version,applied_at) VALUES('9999_future',0)`); err != nil {
		t.Fatal(err)
	}
	store.Close()
	if future, err := sqlite.Open(ctx, path); err == nil {
		future.Close()
		t.Fatal("future schema accepted")
	}
}

// TestPersistentRateLimits 验证登录次数限制在重启后仍生效，等待时长随时间减少，到期后允许再次尝试。
func TestPersistentRateLimits(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "data", "epay.db")
	store, err := sqlite.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { store.Close() }()
	now := time.Now().Truncate(time.Second)
	for i := 0; i < 5; i++ {
		if ok, _, err := store.ReserveLoginAttempt(ctx, "peer:a", now); err != nil || !ok {
			t.Fatalf("attempt %d: %v", i, err)
		}
	}
	store.Close()
	store, err = sqlite.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if ok, wait, err := store.ReserveLoginAttempt(ctx, "peer:a", now); err != nil || ok || wait != 15*time.Minute {
		t.Fatalf("limit lost on restart: wait=%v, err=%v", wait, err)
	}
	if ok, wait, err := store.ReserveLoginAttempt(ctx, "peer:a", now.Add(2*time.Minute)); err != nil || ok || wait != 13*time.Minute {
		t.Fatalf("remaining delay must decrease without extending lock: wait=%v, err=%v", wait, err)
	}
	if ok, _, err := store.ReserveLoginAttempt(ctx, "peer:a", now.Add(16*time.Minute)); err != nil || !ok {
		t.Fatalf("lock did not expire: %v", err)
	}
	for i := 0; i < 19; i++ {
		if ok, _, err := store.ReserveLoginAttempt(ctx, string(rune('b'+i)), now.Add(16*time.Minute)); err != nil || !ok {
			t.Fatalf("global attempt %d: %v", i, err)
		}
	}
	if ok, wait, err := store.ReserveLoginAttempt(ctx, "new-peer", now.Add(16*time.Minute)); err != nil || ok || wait != 15*time.Minute {
		t.Fatalf("global limit bypassed: wait=%v, err=%v", wait, err)
	}
}
