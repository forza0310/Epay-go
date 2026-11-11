// 本文件验证 RFC 向量、双因素、防重放、恢复撤销与跨进程状态持久化。
package admin_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"database/sql"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"epay/internal/admin"
	"epay/internal/securebox"
	"epay/internal/storage/sqlite"
)

const testPassword = "correct horse battery staple"
const testSecret = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"

// TestPasswordPolicyAcrossAuthenticationFlows 验证初始化、登录和验证器恢复使用相同的密码长度及编码要求，避免创建后无法登录。
func TestPasswordPolicyAcrossAuthenticationFlows(t *testing.T) {
	for _, tc := range []struct {
		name     string
		password string
		accepted bool
	}{
		{"9_bytes", strings.Repeat("a", 9), false},
		{"10_bytes", strings.Repeat("a", 10), true},
		{"11_bytes", strings.Repeat("a", 11), true},
		{"12_bytes", strings.Repeat("a", 12), true},
		{"1024_bytes", strings.Repeat("a", 1024), true},
		{"1025_bytes", strings.Repeat("a", 1025), false},
		{"invalid_utf8", strings.Repeat("a", 10) + "\xff", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "data", "epay.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			box, err := securebox.New([]byte("01234567890123456789012345678901"))
			if err != nil {
				t.Fatal(err)
			}
			service, err := admin.New(store, box)
			if err != nil {
				t.Fatal(err)
			}
			now := time.Now()
			err = service.Initialize(ctx, tc.password, testSecret, testCode(testSecret, now))
			if !tc.accepted {
				if !errors.Is(err, admin.ErrWeakPassword) {
					t.Fatalf("invalid password policy: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := service.Login(ctx, "127.0.0.1", tc.password, testCode(testSecret, now.Add(30*time.Second))); err != nil {
				t.Fatalf("initialized password rejected at login: %v", err)
			}
			if err := service.VerifyRecoveryPassword(ctx, tc.password); err != nil {
				t.Fatalf("initialized password rejected at recovery: %v", err)
			}
			secret, err := admin.NewTOTPSecret()
			if err != nil {
				t.Fatal(err)
			}
			if err := service.ResetTOTP(ctx, tc.password, secret, testCode(secret, time.Now())); err != nil {
				t.Fatalf("initialized password rejected at TOTP reset: %v", err)
			}
		})
	}
}

// testCode 根据测试绑定密钥和指定时间计算动态码，供登录及恢复测试使用。
func testCode(secret string, now time.Time) string {
	raw, _ := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	var count [8]byte
	binary.BigEndian.PutUint64(count[:], uint64(now.Unix()/30))
	mac := hmac.New(sha1.New, raw)
	mac.Write(count[:])
	sum := mac.Sum(nil)
	off := sum[19] & 15
	return fmt.Sprintf("%06d", (binary.BigEndian.Uint32(sum[off:off+4])&0x7fffffff)%1000000)
}

// TestTOTPRFC6238 用 RFC 6238 的已知输入和预期结果检查动态码算法，并验证格式错误或超过允许时间的验证码会被拒绝。
func TestTOTPRFC6238(t *testing.T) {
	for _, tc := range []struct {
		stamp int64
		code  string
	}{{59, "287082"}, {1111111109, "081804"}, {1111111111, "050471"}, {1234567890, "005924"}, {2000000000, "279037"}, {20000000000, "353130"}} {
		step, err := admin.MatchTOTP(testSecret, tc.code, time.Unix(tc.stamp, 0))
		if err != nil || step != tc.stamp/30 {
			t.Fatalf("RFC vector %d: %d %v", tc.stamp, step, err)
		}
	}
	for _, code := range []string{"", "12345", "1234567", "abcdef", "２８７０８２", "000000"} {
		if _, err := admin.MatchTOTP(testSecret, code, time.Unix(59, 0)); err == nil {
			t.Fatalf("invalid code accepted: %q", code)
		}
	}
	if _, err := admin.MatchTOTP(testSecret, "287082", time.Unix(150, 0)); err == nil {
		t.Fatal("expired code accepted")
	}
}

// TestAdministratorLifecycle 验证同一动态码不能被并发或重复使用、会话可跨重启保留，以及重置绑定、退出和过期会使会话失效。
func TestAdministratorLifecycle(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "data", "epay.db")
	store, err := sqlite.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { store.Close() }()
	box, _ := securebox.New([]byte("01234567890123456789012345678901"))
	service, err := admin.New(store, box)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err = service.Initialize(ctx, testPassword, testSecret, testCode(testSecret, now)); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(service.Initialize(ctx, testPassword, testSecret, "000000"), admin.ErrAlreadyInitialized) {
		t.Fatal("administrator overwritten")
	}
	if _, err = service.Login(ctx, "127.0.0.1", "incorrect password", testCode(testSecret, now.Add(30*time.Second))); !errors.Is(err, admin.ErrCredentials) {
		t.Fatalf("wrong password: %v", err)
	}
	if _, err = service.Login(ctx, "127.0.0.1", testPassword, ""); !errors.Is(err, admin.ErrCredentials) {
		t.Fatalf("missing second factor: %v", err)
	}
	// 两个并发请求只能有一个成功消费当前动态码。
	var wg sync.WaitGroup
	results := make(chan admin.Session, 2)
	failures := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, e := service.Login(ctx, "127.0.0.1", testPassword, testCode(testSecret, now.Add(30*time.Second)))
			if e != nil {
				failures <- e
			} else {
				results <- s
			}
		}()
	}
	wg.Wait()
	close(results)
	close(failures)
	if len(results) != 1 || len(failures) != 1 {
		t.Fatalf("login successes=%d failures=%d", len(results), len(failures))
	}
	session := <-results
	for err := range failures {
		if !errors.Is(err, admin.ErrCredentials) {
			t.Fatal(err)
		}
	}
	if valid, err := service.ValidCSRF(ctx, session.Token, session.CSRFToken); err != nil || !valid {
		t.Fatalf("CSRF: %v %v", valid, err)
	}
	if valid, _ := service.ValidCSRF(ctx, session.Token, session.Token); valid {
		t.Fatal("unbound CSRF accepted")
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = sqlite.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	service, _ = admin.New(store, box)
	if valid, err := service.ValidSession(ctx, session.Token); err != nil || !valid {
		t.Fatalf("session after restart: %v %v", valid, err)
	}
	if _, err = service.Login(ctx, "another-peer", testPassword, testCode(testSecret, now.Add(30*time.Second))); !errors.Is(err, admin.ErrCredentials) {
		t.Fatalf("replay after restart: %v", err)
	}
	nextSecret, err := admin.NewTOTPSecret()
	if err != nil {
		t.Fatal(err)
	}
	if err = service.ResetTOTP(ctx, "wrong password", nextSecret, testCode(nextSecret, time.Now())); !errors.Is(err, admin.ErrCredentials) {
		t.Fatalf("recovery password bypass: %v", err)
	}
	if err = service.ResetTOTP(ctx, testPassword, nextSecret, testCode(nextSecret, time.Now())); err != nil {
		t.Fatal(err)
	}
	if valid, _ := service.ValidSession(ctx, session.Token); valid {
		t.Fatal("recovery did not revoke session")
	}
	session, err = service.Login(ctx, "recovered-peer", testPassword, testCode(nextSecret, time.Now().Add(30*time.Second)))
	if err != nil {
		t.Fatal(err)
	}
	if err = service.Logout(ctx, session.Token); err != nil {
		t.Fatal(err)
	}
	if valid, _ := service.ValidSession(ctx, session.Token); valid {
		t.Fatal("logout failed")
	}
	// 不等待实际时钟推进，直接构造已过期的数据库状态验证时间边界。
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec(`UPDATE owner SET last_totp_step=-1`); err != nil {
		t.Fatal(err)
	}
	session, err = service.Login(ctx, "expiry-peer", testPassword, testCode(nextSecret, time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`UPDATE admin_sessions SET expires_at=?`, time.Now().Add(-time.Second).Unix()); err != nil {
		t.Fatal(err)
	}
	if valid, _ := service.ValidSession(ctx, session.Token); valid {
		t.Fatal("expired session accepted")
	}
}
