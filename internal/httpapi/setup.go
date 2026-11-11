// 本文件提供仅未初始化时可用的管理员创建页；主密钥始终留在服务端。
package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"epay/internal/admin"
)

// setupForm 由进程临时密钥加密到 Cookie，绑定种子与十分钟 CSRF 令牌。
// 不保存密码；重启会使未提交的绑定流程失效。
type setupForm struct{ Secret, CSRF string }
type setupView struct{ Secret, CSRF, Error string }

// setupPage 显示首次初始化页面，并生成或复用验证器绑定信息；管理员已存在时跳转到登录页。
func (h *Handler) setupPage(w http.ResponseWriter, r *http.Request) {
	initialized, err := h.admin.Initialized(r.Context())
	if err != nil {
		h.internalError(w, "setup_state", err)
		return
	}
	if initialized {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}
	form, ok := h.readSetupForm(r)
	if !ok {
		form.Secret, err = admin.NewTOTPSecret()
		if err != nil {
			h.internalError(w, "setup_seed", err)
			return
		}
		form.CSRF, err = h.loginToken()
		if err != nil {
			h.internalError(w, "setup_csrf", err)
			return
		}
		plain, err := json.Marshal(form)
		if err != nil {
			h.internalError(w, "setup_form", err)
			return
		}
		sealed, err := h.setupBox.Seal("admin:setup", plain)
		if err != nil {
			h.internalError(w, "setup_form", err)
			return
		}
		h.setCookie(w, "setup", base64.RawURLEncoding.EncodeToString(sealed), time.Now().Add(10*time.Minute), 600)
	}
	render(w, http.StatusOK, setupTemplate, setupView{Secret: form.Secret, CSRF: form.CSRF})
}

// readSetupForm 从加密 Cookie 中读取绑定信息，并检查表单是否仍有效；被修改、已过期或来自其他进程时返回失败。
func (h *Handler) readSetupForm(r *http.Request) (setupForm, bool) {
	var form setupForm
	cookie, err := r.Cookie(h.cookieName("setup"))
	if err != nil {
		return form, false
	}
	sealed, err := base64.RawURLEncoding.DecodeString(cookie.Value)
	if err != nil {
		return form, false
	}
	plain, err := h.setupBox.Open("admin:setup", sealed)
	if err != nil || json.Unmarshal(plain, &form) != nil {
		return setupForm{}, false
	}
	return form, h.validLoginToken(form.CSRF)
}

// setup 检查表单来源、密码和验证器绑定信息，调用管理员服务完成首次创建。
func (h *Handler) setup(w http.ResponseWriter, r *http.Request) {
	initialized, err := h.admin.Initialized(r.Context())
	if err != nil {
		h.internalError(w, "setup_state", err)
		return
	}
	if initialized {
		writeError(w, http.StatusConflict, "管理员已创建，初始化入口已关闭")
		return
	}
	values, ok := h.parseForm(w, r, "password", "password_confirm", "code", "csrf_token")
	if !ok {
		return
	}
	form, ok := h.readSetupForm(r)
	if !ok || !sameSecret(form.CSRF, values.Get("csrf_token")) {
		writeError(w, http.StatusForbidden, "初始化表单已失效，请刷新页面并重新绑定验证器")
		return
	}
	fail := func(status int, message string) {
		render(w, status, setupTemplate, setupView{Secret: form.Secret, CSRF: form.CSRF, Error: message})
	}
	password := values.Get("password")
	if password != values.Get("password_confirm") {
		fail(http.StatusBadRequest, "两次密码不一致")
		return
	}
	if admin.ValidatePassword(password) != nil {
		fail(http.StatusBadRequest, "密码须为 10–1024 字节的有效 UTF-8 文本")
		return
	}
	err = h.admin.Initialize(r.Context(), password, form.Secret, values.Get("code"))
	switch {
	case errors.Is(err, admin.ErrAlreadyInitialized):
		writeError(w, http.StatusConflict, "管理员已创建，初始化入口已关闭")
	case errors.Is(err, admin.ErrRateLimited):
		w.Header().Set("Retry-After", "1")
		fail(http.StatusTooManyRequests, "当前请求较多，请等待 1 秒后重试")
	case errors.Is(err, admin.ErrCredentials):
		fail(http.StatusBadRequest, "动态码不正确，请检查验证器绑定和设备时间")
	case err != nil:
		h.internalError(w, "admin_setup", err)
	default:
		h.setCookie(w, "setup", "", time.Time{}, -1)
		render(w, http.StatusCreated, setupCompleteTemplate, nil)
	}
}
