// 本文件统一检查后台请求的登录状态，并向受保护的处理函数传递已验证的会话。

package httpapi

import "net/http"

// adminSession 保存通过登录检查后得到的会话令牌和 CSRF 令牌，仅供后台处理函数使用。
type adminSession struct {
	Token     string
	CSRFToken string
}

// adminHandler 是需要管理员登录的处理函数；必须经过 requireLogin 包装后才能注册为 HTTP 路由。
type adminHandler func(http.ResponseWriter, *http.Request, adminSession)

// requireLogin 先检查登录状态，通过后将会话传给后台处理函数；失败时不再执行后续操作。
func (h *Handler) requireLogin(next adminHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, csrf, ok := h.authenticate(w, r)
		if !ok {
			return
		}
		next(w, r, adminSession{Token: token, CSRFToken: csrf})
	}
}

// authenticate 检查后台请求的登录会话和 CSRF Cookie；未登录或会话过期时跳转到登录页。
func (h *Handler) authenticate(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	session, err := r.Cookie(h.cookieName("session"))
	if err != nil {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return "", "", false
	}
	valid, err := h.admin.ValidSession(r.Context(), session.Value)
	if err != nil {
		h.internalError(w, "session_check", err)
		return "", "", false
	}
	if !valid {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return "", "", false
	}
	csrf, err := r.Cookie(h.cookieName("csrf"))
	if err != nil {
		writeError(w, http.StatusForbidden, "会话缺少表单令牌，请重新登录")
		return "", "", false
	}
	valid, err = h.admin.ValidCSRF(r.Context(), session.Value, csrf.Value)
	if err != nil {
		h.internalError(w, "csrf_check", err)
		return "", "", false
	}
	if !valid {
		writeError(w, http.StatusForbidden, "会话表单令牌无效")
		return "", "", false
	}
	return session.Value, csrf.Value, true
}
