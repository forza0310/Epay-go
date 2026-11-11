// 本文件通过真实仓储验证后台 HTTP 安全边界及配置管理闭环，不调用支付宝。
package httpapi_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"image/png"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"epay/internal/admin"
	"epay/internal/channel"
	"epay/internal/httpapi"
	"epay/internal/securebox"
	"epay/internal/storage/sqlite"
)

// otp 使用固定的测试绑定密钥，计算指定时间的动态码。
func otp(now time.Time) string {
	return otpFor("GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ", now)
}

// otpFor 使用指定的测试绑定密钥和时间计算动态码，供初始化与登录测试使用。
func otpFor(secret string, now time.Time) string {
	raw, _ := base32.StdEncoding.DecodeString(secret)
	var counter [8]byte
	binary.BigEndian.PutUint64(counter[:], uint64(now.Unix()/30))
	m := hmac.New(sha1.New, raw)
	m.Write(counter[:])
	v := m.Sum(nil)
	i := v[19] & 15
	return fmt.Sprintf("%06d", (binary.BigEndian.Uint32(v[i:i+4])&0x7fffffff)%1000000)
}

// TestAdministratorHTTPBoundary 验证所有受保护路由都先检查会话，正常登录后仍可管理配置和退出。
// 同时检查无效 Cookie、表单 CSRF、来源和限流提示，避免重构后遗漏访问限制。
func TestAdministratorHTTPBoundary(t *testing.T) {
	const origin = "http://127.0.0.1:8080"
	ctx := context.Background()
	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "data", "epay.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	box, _ := securebox.New([]byte("01234567890123456789012345678901"))
	service, _ := admin.New(store, box)
	vault, _ := channel.NewVault(store, box, []string{"alipaycode"})
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	now := time.Now()
	if err = service.Initialize(ctx, "correct horse battery staple", "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ", otp(now)); err != nil {
		t.Fatal(err)
	}
	h, err := httpapi.NewHandler(service, vault, []string{"alipaycode"}, false, logger)
	if err != nil {
		t.Fatal(err)
	}
	routes := h.Routes()
	cookies := map[string]*http.Cookie{}
	request := func(method, target, body, origin string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "http://127.0.0.1:8080"+target, strings.NewReader(body))
		req.RemoteAddr = "127.0.0.1:12345"
		if method == http.MethodPost {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		for _, cookie := range cookies {
			req.AddCookie(cookie)
		}
		rec := httptest.NewRecorder()
		routes.ServeHTTP(rec, req)
		return rec
	}
	saveCookies := func(rec *httptest.ResponseRecorder) {
		for _, cookie := range rec.Result().Cookies() {
			if cookie.MaxAge < 0 {
				delete(cookies, cookie.Name)
			} else {
				cookies[cookie.Name] = cookie
			}
		}
	}
	// 覆盖所有受保护路由，包括写接口，确认鉴权失败时不会进入业务处理。
	protected := []struct{ method, path string }{
		{"GET", "/admin/"}, {"GET", "/admin/channels/new"}, {"GET", "/admin/channels/missing"},
		{"POST", "/admin/channels"}, {"POST", "/admin/logout"},
	}
	assertProtected := func(want int) {
		t.Helper()
		for _, route := range protected {
			rec := request(route.method, route.path, "", origin)
			if rec.Code != want {
				t.Fatalf("protected %s %s: got %d, want %d", route.method, route.path, rec.Code, want)
			}
			if want == http.StatusSeeOther && rec.Header().Get("Location") != "/admin/login" {
				t.Fatal("invalid login redirect")
			}
		}
	}
	assertProtected(http.StatusSeeOther)
	cookies["epay_session"] = &http.Cookie{Name: "epay_session", Value: strings.Repeat("A", 43)}
	cookies["epay_csrf"] = &http.Cookie{Name: "epay_csrf", Value: strings.Repeat("B", 43)}
	assertProtected(http.StatusSeeOther)
	delete(cookies, "epay_session")
	delete(cookies, "epay_csrf")
	if channels, err := vault.List(ctx); err != nil || len(channels) != 0 {
		t.Fatal("unauthenticated request changed channels", err)
	}
	rec := request("GET", "/admin/login", "", "")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `name="code"`) {
		t.Fatal("login page missing TOTP")
	}
	if rec.Header().Get("Referrer-Policy") != "same-origin" {
		t.Fatal("login policy must preserve the Origin of same-origin HTML form submissions")
	}
	saveCookies(rec)
	csrf := cookies["epay_login_csrf"].Value
	login := url.Values{"password": {"correct horse battery staple"}, "code": {otp(now.Add(30 * time.Second))}, "csrf_token": {csrf}}
	for _, tc := range []struct {
		name, target, body, origin string
		status                     int
	}{
		{"duplicate", "/admin/login", login.Encode() + "&password=other", "", 400},
		{"query credentials", "/admin/login?password=other", login.Encode(), "", 400},
		{"cross origin", "/admin/login", login.Encode(), "https://evil.example", 403},
		{"opaque origin", "/admin/login", login.Encode(), "null", 403},
		{"different port", "/admin/login", login.Encode(), "http://127.0.0.1:8081", 403},
		{"different scheme", "/admin/login", login.Encode(), "https://127.0.0.1:8080", 403},
		{"missing csrf", "/admin/login", "password=irrelevant", "", 403},
		{"unknown field", "/admin/login", login.Encode() + "&extra=x", "", 400},
		{"oversized", "/admin/login", "password=" + strings.Repeat("x", 65<<10), "", 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := request("POST", tc.target, tc.body, tc.origin)
			if rec.Code != tc.status {
				t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
			}
		})
	}
	bad := url.Values{"password": {"incorrect password"}, "code": {otp(now.Add(30 * time.Second))}, "csrf_token": {csrf}}
	if rec = request("POST", "/admin/login", bad.Encode(), ""); rec.Code != 401 {
		t.Fatalf("bad credentials: %d", rec.Code)
	}
	rec = request("POST", "/admin/login", login.Encode(), origin)
	if rec.Code != 303 {
		t.Fatalf("login: %d %s", rec.Code, rec.Body.String())
	}
	saveCookies(rec)
	session := cookies["epay_session"]
	if !session.HttpOnly || session.SameSite != http.SameSiteStrictMode || session.Path != "/" {
		t.Fatal("unsafe session cookie")
	}
	if rec = request("GET", "/admin/", "", ""); rec.Code != 200 {
		t.Fatalf("dashboard: %d", rec.Code)
	}
	csrfCookie := cookies["epay_csrf"]
	delete(cookies, "epay_csrf")
	assertProtected(http.StatusForbidden)
	cookies["epay_csrf"] = &http.Cookie{Name: "epay_csrf", Value: strings.Repeat("B", 43)}
	assertProtected(http.StatusForbidden)
	cookies["epay_csrf"] = csrfCookie
	if rec = request("GET", "/admin/channels/new", "", ""); rec.Code != 200 || !strings.Contains(rec.Body.String(), csrfCookie.Value) {
		t.Fatal("verified session was not passed to form handler")
	}
	config := `{"private_key":"fixture-secret-never-return"}`
	form := url.Values{"csrf_token": {cookies["epay_csrf"].Value}, "id": {""}, "name": {"<script>alert(1)</script>"}, "account_id": {"2088"}, "plugin_id": {"alipaycode"}, "enabled": {"true"}, "config": {config}}
	invalid := url.Values{}
	for k, v := range form {
		invalid[k] = v
	}
	invalid.Set("csrf_token", "bad")
	if rec = request("POST", "/admin/channels", invalid.Encode(), ""); rec.Code != 403 {
		t.Fatalf("config CSRF: %d", rec.Code)
	}
	invalid.Set("csrf_token", form.Get("csrf_token"))
	invalid.Set("plugin_id", "uncompiled")
	if rec = request("POST", "/admin/channels", invalid.Encode(), ""); rec.Code != 400 {
		t.Fatalf("plugin whitelist: %d", rec.Code)
	}
	rec = request("POST", "/admin/channels", form.Encode(), origin)
	if rec.Code != 303 {
		t.Fatalf("save: %d %s", rec.Code, rec.Body.String())
	}
	channels, err := vault.List(ctx)
	if err != nil || len(channels) != 1 {
		t.Fatalf("list: %v %v", channels, err)
	}
	id := channels[0].ID
	for _, path := range []string{"/admin/", "/admin/channels/" + id} {
		rec = request("GET", path, "", "")
		if rec.Code != 200 || strings.Contains(rec.Body.String(), "fixture-secret-never-return") || strings.Contains(rec.Body.String(), "<script>") {
			t.Fatalf("unsafe response %s: %s", path, rec.Body.String())
		}
	}
	form.Set("id", id)
	form.Set("enabled", "false")
	form.Set("config", "")
	if rec = request("POST", "/admin/channels", form.Encode(), origin); rec.Code != 303 {
		t.Fatalf("update: %d", rec.Code)
	}
	c, plain, err := vault.Load(ctx, id)
	if err != nil || c.Enabled || string(plain) != config {
		t.Fatalf("retained secret or disabled state lost: %v", err)
	}
	if rec = request("POST", "/admin/logout", "csrf_token=bad", ""); rec.Code != 403 {
		t.Fatalf("logout CSRF: %d", rec.Code)
	}
	rec = request("POST", "/admin/logout", url.Values{"csrf_token": {cookies["epay_csrf"].Value}}.Encode(), origin)
	if rec.Code != 303 {
		t.Fatal("logout failed")
	}
	// 故意重用注销前 Cookie，验证服务端撤销而不是仅让浏览器删除。
	assertProtected(http.StatusSeeOther)
	if c, plain, err := vault.Load(ctx, id); err != nil || c.Enabled || string(plain) != config {
		t.Fatal("revoked request changed saved channel", err)
	}
	for _, path := range []string{"/api.php?act=balance", "/api.php?act=order", "/submit.php"} {
		if rec = request("GET", path, "", ""); rec.Code != 501 {
			t.Fatalf("stage boundary %s: %d", path, rec.Code)
		}
		if rec.Header().Get("Referrer-Policy") != "no-referrer" {
			t.Fatalf("payment route must not disclose referrers: %s", path)
		}
	}
	// 安全部署使用 Host 前缀，防止父域 Cookie 覆盖。
	secureHandler, err := httpapi.NewHandler(service, vault, []string{"alipaycode"}, true, logger)
	if err != nil {
		t.Fatal(err)
	}
	secureRec := httptest.NewRecorder()
	secureHandler.Routes().ServeHTTP(secureRec, httptest.NewRequest("GET", "https://example.com/admin/login", nil))
	if secureRec.Header().Get("Referrer-Policy") != "same-origin" {
		t.Fatal("HTTPS login must also preserve same-origin form Origin")
	}
	cs := secureRec.Result().Cookies()
	if len(cs) != 1 || !cs[0].Secure || !cs[0].HttpOnly || cs[0].Name != "__Host-epay_login_csrf" || cs[0].Domain != "" || cs[0].Path != "/" {
		t.Fatal("secure cookie policy failed")
	}

	// 用临时数据库中的历史时刻建立限流，验证页面展示剩余时长，而非固定 900 秒。
	lockedAt := time.Now().Add(-2 * time.Minute).Truncate(time.Second)
	for i := 0; i < 5; i++ {
		if allowed, _, err := store.ReserveLoginAttempt(ctx, "peer:127.0.0.1", lockedAt); err != nil || !allowed {
			t.Fatalf("prepare throttle: %v", err)
		}
	}
	if allowed, _, err := store.ReserveLoginAttempt(ctx, "peer:127.0.0.1", lockedAt); err != nil || allowed {
		t.Fatalf("prepare lock: %v", err)
	}
	saveCookies(request("GET", "/admin/login", "", ""))
	limitedForm := url.Values{"password": {"incorrect password"}, "code": {"000000"}, "csrf_token": {cookies["epay_login_csrf"].Value}}
	rec = request("POST", "/admin/login", limitedForm.Encode(), origin)
	waitSeconds, err := strconv.Atoi(rec.Header().Get("Retry-After"))
	if rec.Code != http.StatusTooManyRequests || err != nil || waitSeconds < 775 || waitSeconds > 780 {
		t.Fatalf("rate-limit response: status=%d, retry=%q", rec.Code, rec.Header().Get("Retry-After"))
	}
	delay := fmt.Sprintf("%d 分 %d 秒", waitSeconds/60, waitSeconds%60)
	if waitSeconds%60 == 0 {
		delay = fmt.Sprintf("%d 分钟", waitSeconds/60)
	}
	if !strings.Contains(rec.Body.String(), "登录请求过于频繁") || !strings.Contains(rec.Body.String(), delay) || !strings.Contains(rec.Body.String(), "刷新登录页") {
		t.Fatal("rate-limit page must explain the actual retry delay and refresh requirement")
	}
}

// TestWebInitialization 验证网页初始化和二维码访问受会话及同源检查保护，并发请求只能创建一名管理员，重启后初始化入口仍关闭。
func TestWebInitialization(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "data", "epay.db")
	store, err := sqlite.Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { store.Close() }()
	box, _ := securebox.New([]byte("01234567890123456789012345678901"))
	service, _ := admin.New(store, box)
	vault, _ := channel.NewVault(store, box, []string{"alipaycode"})
	newHandler := func() http.Handler {
		h, err := httpapi.NewHandler(service, vault, []string{"alipaycode"}, true, slog.New(slog.NewTextHandler(io.Discard, nil)))
		if err != nil {
			t.Fatal(err)
		}
		return h.Routes()
	}
	routes := newHandler()
	const origin = "https://pay.example.test"
	request := func(handler http.Handler, method, path, body, from string, cookie *http.Cookie) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, origin+path, strings.NewReader(body))
		req.RemoteAddr = "203.0.113.20:12345"
		if method == "POST" {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.Header.Set("Origin", from)
		}
		if cookie != nil {
			req.AddCookie(cookie)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}
	if rec := request(routes, "GET", "/admin/login", "", "", nil); rec.Code != 303 || rec.Header().Get("Location") != "/admin/setup" {
		t.Fatal("missing setup redirect")
	}
	rec := request(routes, "GET", "/admin/setup", "", "", nil)
	if rec.Code != 200 {
		t.Fatal("setup page", rec.Code)
	}
	match := regexp.MustCompile(`<code>([A-Z2-7]+)</code>`).FindStringSubmatch(rec.Body.String())
	csrf := regexp.MustCompile(`name="csrf_token" value="([^"]+)"`).FindStringSubmatch(rec.Body.String())
	if len(match) != 2 || len(csrf) != 2 {
		t.Fatal("missing binding fields")
	}
	secret := match[1]
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatal("missing setup cookie")
	}
	cookie := cookies[0]
	if !cookie.Secure || !cookie.HttpOnly || cookie.MaxAge != 600 || strings.Contains(cookie.Value, secret) {
		t.Fatal("unsafe setup cookie")
	}

	if !strings.Contains(rec.Body.String(), `src="/admin/setup/qr.png"`) || !strings.Contains(rec.Header().Get("Content-Security-Policy"), "img-src 'self'") {
		t.Fatal("QR image missing or blocked by CSP")
	}
	qr := request(routes, "GET", "/admin/setup/qr.png", "", "", cookie)
	if qr.Code != 200 || qr.Header().Get("Content-Type") != "image/png" || qr.Header().Get("Cache-Control") != "no-store" || qr.Header().Get("Cross-Origin-Resource-Policy") != "same-origin" {
		t.Fatal("unsafe QR response", qr.Code)
	}
	bitmap, err := png.Decode(bytes.NewReader(qr.Body.Bytes()))
	if err != nil || bitmap.Bounds().Dx() != 256 || bitmap.Bounds().Dy() != 256 {
		t.Fatal("invalid QR PNG", err)
	}
	if repeat := request(routes, "GET", "/admin/setup/qr.png", "", "", cookie); !bytes.Equal(qr.Body.Bytes(), repeat.Body.Bytes()) {
		t.Fatal("binding QR changed during setup")
	}
	for _, badCookie := range []*http.Cookie{nil, {Name: cookie.Name, Value: cookie.Value + "x"}} {
		if r := request(routes, "GET", "/admin/setup/qr.png", "", "", badCookie); r.Code != 403 {
			t.Fatal("QR accepted missing or invalid cookie")
		}
	}
	if r := request(newHandler(), "GET", "/admin/setup/qr.png", "", "", cookie); r.Code != 403 {
		t.Fatal("QR accepted old process cookie")
	}
	if r := request(routes, "GET", "/admin/setup/qr.png?secret=untrusted", "", "", cookie); r.Code != 400 {
		t.Fatal("QR accepted query parameters")
	}
	crossReq := httptest.NewRequest("GET", origin+"/admin/setup/qr.png", nil)
	crossReq.AddCookie(cookie)
	crossReq.Header.Set("Sec-Fetch-Site", "cross-site")
	crossRec := httptest.NewRecorder()
	routes.ServeHTTP(crossRec, crossReq)
	if crossRec.Code != 403 {
		t.Fatal("QR allowed cross-site embedding")
	}
	form := url.Values{"password": {"test-password-123"}, "password_confirm": {"test-password-123"}, "code": {otpFor(secret, time.Now())}, "csrf_token": {csrf[1]}}
	for _, test := range []struct {
		name, from string
		cookie     *http.Cookie
	}{
		{"cross origin", "https://evil.example", cookie},
		{"missing cookie", origin, nil},
		{"tampered cookie", origin, &http.Cookie{Name: cookie.Name, Value: cookie.Value + "x"}},
	} {
		if r := request(routes, "POST", "/admin/setup", form.Encode(), test.from, test.cookie); r.Code != 403 {
			t.Fatalf("%s status %d", test.name, r.Code)
		}
	}
	form.Set("csrf_token", "wrong")
	if r := request(routes, "POST", "/admin/setup", form.Encode(), origin, cookie); r.Code != 403 {
		t.Fatal("CSRF accepted")
	}
	form.Set("csrf_token", csrf[1])
	form.Set("password_confirm", "wrong")
	if r := request(routes, "POST", "/admin/setup", form.Encode(), origin, cookie); r.Code != 400 {
		t.Fatal("password mismatch accepted")
	}
	form.Set("password_confirm", "test-password-123")
	form.Set("code", "bad")
	if r := request(routes, "POST", "/admin/setup", form.Encode(), origin, cookie); r.Code != 400 {
		t.Fatal("invalid OTP accepted")
	}
	if initialized, _ := service.Initialized(ctx); initialized {
		t.Fatal("failed request persisted administrator")
	}
	form.Set("code", otpFor(secret, time.Now()))
	if r := request(newHandler(), "POST", "/admin/setup", form.Encode(), origin, cookie); r.Code != 403 {
		t.Fatal("old process cookie accepted")
	}
	var wg sync.WaitGroup
	statuses := make(chan int, 2)
	for range 2 {
		wg.Go(func() { statuses <- request(routes, "POST", "/admin/setup", form.Encode(), origin, cookie).Code })
	}
	wg.Wait()
	close(statuses)
	counts := map[int]int{}
	for status := range statuses {
		counts[status]++
	}
	if counts[201] != 1 || counts[409] != 1 {
		t.Fatalf("creation not unique: %v", counts)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = sqlite.Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	service, _ = admin.New(store, box)
	vault, _ = channel.NewVault(store, box, []string{"alipaycode"})
	routes = newHandler()
	if r := request(routes, "GET", "/admin/setup", "", "", nil); r.Code != 303 || r.Header().Get("Location") != "/admin/login" {
		t.Fatal("setup reopened after restart")
	}
	if r := request(routes, "POST", "/admin/setup", form.Encode(), origin, cookie); r.Code != 409 {
		t.Fatal("administrator overwrite allowed")
	}
	if r := request(routes, "GET", "/admin/setup/qr.png", "", "", cookie); r.Code != 409 {
		t.Fatal("QR remained available after initialization")
	}
	loginPage := request(routes, "GET", "/admin/login", "", "", nil)
	if loginPage.Code != 200 {
		t.Fatal("login unavailable")
	}
	loginCookies := loginPage.Result().Cookies()
	if len(loginCookies) != 1 {
		t.Fatal("missing login CSRF cookie")
	}
	loginForm := url.Values{"password": {"test-password-123"}, "code": {otpFor(secret, time.Now().Add(30*time.Second))}, "csrf_token": {loginCookies[0].Value}}
	if r := request(routes, "POST", "/admin/login", loginForm.Encode(), origin, loginCookies[0]); r.Code != 303 || r.Header().Get("Location") != "/admin/" {
		t.Fatalf("created administrator cannot log in: %d", r.Code)
	}
}
