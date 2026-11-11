// 本文件在内存生成 TOTP 绑定二维码；种子只来自有效初始化 Cookie，不出现在图片 URL 中。
package httpapi

import (
	"net/http"
	"net/url"

	"github.com/skip2/go-qrcode"
)

// setupQRCode 为当前有效的初始化表单生成绑定二维码，不保存图片或允许缓存。
// 管理员已创建或初始化 Cookie 无效时，拒绝返回二维码。
func (h *Handler) setupQRCode(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
	initialized, err := h.admin.Initialized(r.Context())
	if err != nil {
		h.internalError(w, "setup_qr_state", err)
		return
	}
	if initialized {
		writeError(w, http.StatusConflict, "管理员已创建，初始化入口已关闭")
		return
	}
	if r.URL.RawQuery != "" {
		writeError(w, http.StatusBadRequest, "二维码入口不接受查询参数")
		return
	}
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
		writeError(w, http.StatusForbidden, "禁止跨站读取绑定二维码")
		return
	}
	form, ok := h.readSetupForm(r)
	if !ok {
		writeError(w, http.StatusForbidden, "初始化表单已失效，请刷新页面并重新绑定验证器")
		return
	}
	uri := url.URL{Scheme: "otpauth", Host: "totp", Path: "/epay:admin", RawQuery: url.Values{
		"secret": {form.Secret}, "issuer": {"epay"}, "algorithm": {"SHA1"}, "digits": {"6"}, "period": {"30"},
	}.Encode()}
	png, err := qrcode.Encode(uri.String(), qrcode.Medium, 256)
	if err != nil {
		h.internalError(w, "setup_qr_encode", err)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(png)
}
