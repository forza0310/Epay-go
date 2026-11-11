// 本文件负责管理员双因素登录、会话 Cookie、表单 CSRF 与统一安全输入边界。
package httpapi

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"html/template"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"epay/internal/admin"
)

// loginPage 显示登录页面，并生成 10 分钟内有效的表单校验令牌；管理员尚未创建时跳转到初始化页面。
func (h *Handler) loginPage(w http.ResponseWriter, r *http.Request) {
	initialized, err := h.admin.Initialized(r.Context())
	if err != nil {
		h.internalError(w, "admin_state", err)
		return
	}
	if !initialized {
		http.Redirect(w, r, "/admin/setup", http.StatusSeeOther)
		return
	}
	token, err := h.loginToken()
	if err != nil {
		h.internalError(w, "login_form", err)
		return
	}
	h.setCookie(w, "login_csrf", token, time.Now().Add(10*time.Minute), 600)
	render(w, http.StatusOK, loginTemplate, loginView{CSRF: token})
}

// login 检查登录表单、密码和动态码，成功后设置会话 Cookie 并跳转到后台。
func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	form, ok := h.parseForm(w, r, "password", "code", "csrf_token")
	if !ok {
		return
	}
	cookie, err := r.Cookie(h.cookieName("login_csrf"))
	if err != nil || !sameSecret(cookie.Value, form.Get("csrf_token")) || !h.validLoginToken(cookie.Value) {
		writeError(w, http.StatusForbidden, "登录表单已失效，请刷新页面")
		return
	}
	session, err := h.admin.Login(r.Context(), remoteKey(r.RemoteAddr), form.Get("password"), form.Get("code"))
	if err != nil {
		if !errors.Is(err, admin.ErrCredentials) && !errors.Is(err, admin.ErrRateLimited) {
			h.internalError(w, "admin_login", err)
			return
		}
		status := http.StatusUnauthorized
		message := "登录失败，请检查密码和动态码。已用动态码请等待下一周期。"
		var limited *admin.RateLimitError
		if errors.As(err, &limited) {
			status = http.StatusTooManyRequests
			seconds := max(1, int((limited.RetryAfter+time.Second-1)/time.Second))
			w.Header().Set("Retry-After", strconv.Itoa(seconds))
			message = "登录请求过于频繁，请在 " + retryDelayText(seconds) + "后重试。到时请刷新登录页，获取新的表单。"
		}
		render(w, status, loginTemplate, loginView{CSRF: cookie.Value, Error: message})
		return
	}
	maxAge := int(time.Until(session.ExpiresAt).Seconds())
	h.setCookie(w, "session", session.Token, session.ExpiresAt, maxAge)
	h.setCookie(w, "csrf", session.CSRFToken, session.ExpiresAt, maxAge)
	h.setCookie(w, "login_csrf", "", time.Time{}, -1)
	http.Redirect(w, r, "/admin/", http.StatusSeeOther)
}

// retryDelayText 将等待秒数转换为分钟、秒的中文提示；只描述本次响应时的等待时长。
func retryDelayText(seconds int) string {
	if seconds < 60 {
		return fmt.Sprintf("%d 秒", seconds)
	}
	if seconds%60 == 0 {
		return fmt.Sprintf("%d 分钟", seconds/60)
	}
	return fmt.Sprintf("%d 分 %d 秒", seconds/60, seconds%60)
}

// logout 检查提交的表单令牌，删除服务端会话并清除浏览器 Cookie，然后返回登录页。
// 登录状态由 requireLogin 检查，会话通过参数传入。
func (h *Handler) logout(w http.ResponseWriter, r *http.Request, session adminSession) {
	form, ok := h.parseForm(w, r, "csrf_token")
	if !ok || !h.checkCSRF(w, r, session.Token, form.Get("csrf_token")) {
		return
	}
	if err := h.admin.Logout(r.Context(), session.Token); err != nil {
		h.internalError(w, "admin_logout", err)
		return
	}
	for _, name := range []string{"session", "csrf"} {
		h.setCookie(w, name, "", time.Time{}, -1)
	}
	http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
}

// checkCSRF 检查提交的表单令牌是否属于当前会话，不匹配时返回错误响应。
func (h *Handler) checkCSRF(w http.ResponseWriter, r *http.Request, session, csrf string) bool {
	valid, err := h.admin.ValidCSRF(r.Context(), session, csrf)
	if err != nil {
		h.internalError(w, "csrf_check", err)
		return false
	}
	if !valid {
		writeError(w, http.StatusForbidden, "表单令牌无效")
		return false
	}
	return true
}

// parseForm 读取 POST 表单，拒绝跨站请求、URL 查询参数，以及重复或未允许的表单字段。
func (h *Handler) parseForm(w http.ResponseWriter, r *http.Request, fields ...string) (url.Values, bool) {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/x-www-form-urlencoded" || r.URL.RawQuery != "" {
		writeError(w, http.StatusBadRequest, "只接受无查询参数的表单请求")
		return nil, false
	}
	site := r.Header.Get("Sec-Fetch-Site")
	if site != "" && site != "same-origin" && site != "none" {
		writeError(w, http.StatusForbidden, "禁止跨站提交")
		return nil, false
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		scheme := "http"
		if h.cookieSecure {
			scheme = "https"
		}
		if origin != scheme+"://"+r.Host {
			writeError(w, http.StatusForbidden, "请求来源不匹配")
			return nil, false
		}
	}
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "表单无效或过大")
		return nil, false
	}
	allowed := make(map[string]bool, len(fields))
	for _, field := range fields {
		allowed[field] = true
	}
	for field, values := range r.PostForm {
		if !allowed[field] || len(values) != 1 {
			writeError(w, http.StatusBadRequest, "表单包含未知或重复字段")
			return nil, false
		}
	}
	return r.PostForm, true
}

// cookieName 生成 Cookie 名称；启用 Secure 时使用 __Host- 前缀，限制 Cookie 的作用范围。
func (h *Handler) cookieName(name string) string {
	if h.cookieSecure {
		return "__Host-epay_" + name
	}
	return "epay_" + name
}

// setCookie 写入仅供本站使用的 HttpOnly Cookie，统一设置有效期和 Secure、SameSite 属性。
func (h *Handler) setCookie(w http.ResponseWriter, name, value string, expires time.Time, maxAge int) {
	http.SetCookie(w, &http.Cookie{Name: h.cookieName(name), Value: value, Path: "/", Expires: expires, MaxAge: maxAge, HttpOnly: true, Secure: h.cookieSecure, SameSite: http.SameSiteStrictMode})
}

// loginToken 生成包含随机值、签发时间和签名的表单校验令牌。
func (h *Handler) loginToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	value := base64.RawURLEncoding.EncodeToString(raw) + "." + strconv.FormatInt(time.Now().Unix(), 10)
	return value + "." + h.signLogin(value), nil
}

// signLogin 用本次进程的随机密钥签名表单令牌，防止客户端自行修改或伪造。
func (h *Handler) signLogin(value string) string {
	mac := hmac.New(sha256.New, h.loginKey[:])
	mac.Write([]byte(value))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// validLoginToken 检查表单令牌的格式、签名和时间，拒绝超过 10 分钟或由其他进程生成的令牌。
func (h *Handler) validLoginToken(value string) bool {
	parts := strings.Split(value, ".")
	if len(parts) != 3 || len(parts[0]) != 43 {
		return false
	}
	stamp, err := strconv.ParseInt(parts[1], 10, 64)
	age := time.Now().Unix() - stamp
	return err == nil && age >= 0 && age <= 600 && sameSecret(parts[2], h.signLogin(parts[0]+"."+parts[1]))
}

// sameSecret 比较两个非空、等长的敏感字符串，避免比较耗时随匹配位置变化。
func sameSecret(a, b string) bool {
	return a != "" && len(a) == len(b) && subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// remoteKey 从直连地址中提取 IP 作为限流标识，不使用客户端提供的代理头。
func remoteKey(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return "invalid-peer"
	}
	return host
}

// internalError 返回统一错误提示；日志只记录操作名和错误类型，避免泄露密码或配置。
func (h *Handler) internalError(w http.ResponseWriter, operation string, err error) {
	h.logger.Error("administrator operation failed", "operation", operation, "error_type", fmt.Sprintf("%T", err))
	writeError(w, http.StatusInternalServerError, "操作失败，请检查服务状态")
}

// render 先在内存中生成完整 HTML，再写入响应；模板执行失败时返回错误页面。
func render(w http.ResponseWriter, status int, t *template.Template, data any) {
	var body bytes.Buffer
	if err := t.Execute(&body, data); err != nil {
		writeError(w, http.StatusInternalServerError, "页面渲染失败")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(body.Bytes())
}
