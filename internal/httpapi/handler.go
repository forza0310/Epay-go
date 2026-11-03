// Package httpapi 负责路由、HTTP 边界及兼容响应，不在处理器内实现支付或 SQL。
package httpapi

import (
	"encoding/json"
	"net/http"

	"epay/internal/protocol/zpay"
)

// Handler 当前提供可运行的协议骨架；业务尚未接入，不能用于实际收款。
type Handler struct{}

// NewHandler 构造处理器，后续在此注入用例服务和管理员会话服务。
func NewHandler() *Handler { return &Handler{} }

// Routes 显式限制方法，健康检查与业务就绪检查分离。
func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", h.health)
	mux.HandleFunc("GET /readyz", h.ready)
	mux.HandleFunc("GET /submit.php", h.submit)
	mux.HandleFunc("POST /submit.php", h.submit)
	mux.HandleFunc("POST /mapi.php", h.apiPay)
	mux.HandleFunc("GET /api.php", h.api)
	mux.HandleFunc("POST /api.php", h.api)
	mux.HandleFunc("GET /admin/{$}", h.admin)
	return safetyHeaders(mux)
}

// health 仅表示进程能够响应，不表示已经具备收款能力。
func (*Handler) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "stage": "scaffold"})
}

// ready 在持久化、鉴权和支付适配器完成之前保持未就绪。
func (*Handler) ready(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not_ready", "reason": "payment service is scaffolded"})
}

// submit 预留页面跳转支付入口，正式实现需先验签、幂等建单再跳转。
func (*Handler) submit(w http.ResponseWriter, _ *http.Request) {
	notImplemented(w, "页面跳转支付尚未实现")
}

// apiPay 预留服务端下单入口，支持 multipart/form-data 和表单编码的计划见 API 文档。
func (*Handler) apiPay(w http.ResponseWriter, _ *http.Request) {
	notImplemented(w, "API 支付尚未实现")
}

// api 分发查询动作，框架阶段不解析或记录查询字符串中的密钥。
func (*Handler) api(w http.ResponseWriter, r *http.Request) {
	actions := r.URL.Query()["act"]
	if len(actions) != 1 {
		writeError(w, http.StatusBadRequest, "必须提供唯一 act 参数")
		return
	}
	if r.Method != http.MethodGet && actions[0] != "refund" {
		w.Header().Set("Allow", http.MethodGet)
		writeError(w, http.StatusMethodNotAllowed, "查询接口仅支持 GET")
		return
	}
	switch actions[0] {
	case "order":
		notImplemented(w, "订单查询尚未实现")
	case "balance":
		notImplemented(w, "余额查询尚未实现，余额语义待确认")
	case "refund":
		notImplemented(w, "当前版本不提供退款功能")
	default:
		writeError(w, http.StatusBadRequest, "不支持的 act")
	}
}

// admin 预留唯一管理员入口，鉴权未完成前不公开管理功能。
func (*Handler) admin(w http.ResponseWriter, _ *http.Request) {
	notImplemented(w, "管理员后台尚未实现")
}

// notImplemented 对尚未接入的能力返回明确失败，禁止用假成功数据占位。
func notImplemented(w http.ResponseWriter, message string) {
	writeError(w, http.StatusNotImplemented, message)
}
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, zpay.ErrorResponse{Code: "error", Message: message})
}

// writeJSON 只序列化受控的响应结构，调用方不得传入密钥配置。
func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

// safetyHeaders 关闭业务响应缓存，限制请求体并设置浏览器基础防护。
func safetyHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
		next.ServeHTTP(w, r)
	})
}
