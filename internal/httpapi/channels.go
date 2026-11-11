// 本文件提供通道管理页面；登录由路由中间件检查，写操作还须检查提交的表单令牌。
package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"

	"epay/internal/channel"
)

// adminPage 显示收款通道列表，并使用传入会话的 CSRF 令牌生成表单。
func (h *Handler) adminPage(w http.ResponseWriter, r *http.Request, session adminSession) {
	channels, err := h.vault.List(r.Context())
	if err != nil {
		h.internalError(w, "channel_list", err)
		return
	}
	render(w, http.StatusOK, dashboardTemplate, dashboardView{CSRF: session.CSRFToken, Channels: channels})
}

// channelPage 显示新增或编辑通道的表单，使用传入的会话信息，不回显已保存的敏感配置。
func (h *Handler) channelPage(w http.ResponseWriter, r *http.Request, session adminSession) {
	view := channelView{CSRF: session.CSRFToken, Plugins: h.pluginIDs}
	if id := r.PathValue("id"); id != "" {
		channels, err := h.vault.List(r.Context())
		if err != nil {
			h.internalError(w, "channel_list", err)
			return
		}
		found := false
		for _, c := range channels {
			if c.ID == id {
				view.Channel = c
				found = true
				break
			}
		}
		if !found {
			http.NotFound(w, r)
			return
		}
	}
	render(w, http.StatusOK, channelTemplate, view)
}

// saveChannel 检查提交的表单令牌，保存新增或修改的通道配置，成功后返回通道列表。
// 登录状态由 requireLogin 检查，会话通过参数传入。
func (h *Handler) saveChannel(w http.ResponseWriter, r *http.Request, session adminSession) {
	form, ok := h.parseForm(w, r, "csrf_token", "id", "plugin_id", "account_id", "name", "enabled", "config")
	if !ok || !h.checkCSRF(w, r, session.Token, form.Get("csrf_token")) {
		return
	}
	id := form.Get("id")
	if id == "" {
		raw := make([]byte, 16)
		if _, err := rand.Read(raw); err != nil {
			h.internalError(w, "channel_id", err)
			return
		}
		id = hex.EncodeToString(raw)
	} else {
		existing, err := h.vault.List(r.Context())
		if err != nil {
			h.internalError(w, "channel_list", err)
			return
		}
		found := false
		for _, c := range existing {
			if c.ID == id {
				found = true
				break
			}
		}
		if !found {
			http.NotFound(w, r)
			return
		}
	}
	enabled := form.Get("enabled")
	if enabled != "true" && enabled != "false" {
		writeError(w, http.StatusBadRequest, "启用状态无效")
		return
	}
	c := channel.Channel{ID: id, PluginID: form.Get("plugin_id"), AccountID: form.Get("account_id"), Name: form.Get("name"), Enabled: enabled == "true"}
	err := h.vault.Save(r.Context(), c, []byte(form.Get("config")))
	if err != nil {
		if errors.Is(err, channel.ErrInvalidConfig) {
			writeError(w, http.StatusBadRequest, "通道信息或配置无效；配置须为非空 JSON 对象，更换账户或插件时须重新填写")
			return
		}
		h.internalError(w, "channel_save", err)
		return
	}
	http.Redirect(w, r, "/admin/", http.StatusSeeOther)
}
