// Package httpapi 负责路由、HTTP 边界及兼容响应，不在处理器内实现支付或 SQL。
package httpapi

import (
	"crypto/rand"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"epay/internal/admin"
	"epay/internal/channel"
	"epay/internal/protocol/zpay"
	"epay/internal/securebox"
)

// Handler 装配管理员后台与支付协议占位入口，目前不能用于实际收款。
type Handler struct {
	admin        *admin.Service
	cookieSecure bool
	vault        *channel.Vault
	pluginIDs    []string
	loginKey     [32]byte
	setupBox     *securebox.Box
	logger       *slog.Logger
}

// NewHandler 创建 HTTP 请求处理器，具体路由由 Routes 注册。
func NewHandler(service *admin.Service, vault *channel.Vault, pluginIDs []string, secure bool, logger *slog.Logger) (*Handler, error) {
	h := &Handler{admin: service, vault: vault, pluginIDs: append([]string(nil), pluginIDs...), cookieSecure: secure, logger: logger}
	if _, err := rand.Read(h.loginKey[:]); err != nil {
		return nil, err
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	var err error
	h.setupBox, err = securebox.New(key)
	if err != nil {
		return nil, err
	}
	return h, nil
}

// Routes 注册各接口及允许的 HTTP 方法，并为所有请求添加响应头和请求体大小限制。
func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux() // 创建 http.ServeMux，它实现了 ServeHTTP(...)，满足 http.Handler 接口
	mux.HandleFunc("GET /healthz", h.health)
	mux.HandleFunc("GET /readyz", h.ready)
	mux.HandleFunc("GET /submit.php", h.submit)
	mux.HandleFunc("POST /submit.php", h.submit)
	mux.HandleFunc("POST /mapi.php", h.apiPay)
	mux.HandleFunc("GET /api.php", h.api)
	mux.HandleFunc("POST /api.php", h.api)
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/admin/login", http.StatusSeeOther) })
	mux.HandleFunc("GET /admin/setup", h.setupPage)
	mux.HandleFunc("GET /admin/setup/qr.png", h.setupQRCode)
	mux.HandleFunc("POST /admin/setup", h.setup)
	mux.HandleFunc("GET /admin/login", h.loginPage)
	mux.HandleFunc("POST /admin/login", h.login)

	// 后台业务统一经过登录检查，再接收已验证的会话。
	mux.HandleFunc("GET /admin/{$}", h.requireLogin(h.adminPage))
	mux.HandleFunc("POST /admin/logout", h.requireLogin(h.logout))
	mux.HandleFunc("GET /admin/channels/new", h.requireLogin(h.channelPage))
	mux.HandleFunc("GET /admin/channels/{id}", h.requireLogin(h.channelPage))
	mux.HandleFunc("POST /admin/channels", h.requireLogin(h.saveChannel))
	return safetyHeaders(mux)
}

// health 报告进程是否能响应请求，不代表支付功能已经可用。
// 接收者没有写名字，因为方法体并不使用这个 Handler 对象。
func (_ *Handler) health(w http.ResponseWriter, _ *http.Request) {
	// 读取不存在的键时，Go 不会抛出类似 Python KeyError 的异常，而是返回值类型的零值：
	// value := data["missing"] // value是""
	// 如果还想知道键是否存在，使用两个接收变量：
	// value, exists := data["missing"]
	// value  是 ""
	// exists 是 false
	// 这是 Go 很常见的 “值 + 是否存在” 写法。存在但值恰好是 ""，与根本不存在，就能区分开了
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "stage": "admin_foundation"})
}

// ready 在支付功能尚未完成时返回未就绪状态，避免将服务启动误认为已经能够收款。
func (_ *Handler) ready(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not_ready", "reason": "payment service is scaffolded"})
}

// submit 处理页面支付请求；目前尚未实现，返回明确的错误响应。
func (_ *Handler) submit(w http.ResponseWriter, _ *http.Request) {
	notImplemented(w, "页面跳转支付尚未实现")
}

// apiPay 处理 API 下单请求；目前尚未实现，返回明确的错误响应。
func (_ *Handler) apiPay(w http.ResponseWriter, _ *http.Request) {
	notImplemented(w, "API 支付尚未实现")
}

// api 检查 act 参数并选择对应处理分支；当前查询与退款功能尚未实现，不读取或记录调用密钥。
func (_ *Handler) api(w http.ResponseWriter, r *http.Request) {
	actions := r.URL.Query()["act"] // map[string][]string 这个参数出现过的所有值
	// []string 读作“字符串 slice”，它是一种可变长度的序列值，引用底层数组的一段或全部元素，它像是指针。
	// [2]string 读作“字符串数组”，长度固定，赋值会复制元素；数组的长度是类型的一部分。
	if len(actions) != 1 {
		writeError(w, http.StatusBadRequest, "必须提供唯一 act 参数")
		return
	}
	if r.Method != http.MethodGet && actions[0] != "refund" {
		w.Header().Set("Allow", http.MethodGet)
		writeError(w, http.StatusMethodNotAllowed, "查询接口仅支持 GET")
		return
	}
	// go 的 switch 不会贯穿到下一个 case，通常不需要 break
	switch actions[0] {
	case "order":
		notImplemented(w, "订单查询尚未实现")
	case "balance":
		writeError(w, http.StatusNotImplemented, "act=balance 暂不支持")
	case "refund":
		notImplemented(w, "当前版本不提供退款功能")
	default:
		writeError(w, http.StatusBadRequest, "不支持的 act")
	}
}

// notImplemented 以 HTTP 501 告知调用方该功能尚未实现。
func notImplemented(w http.ResponseWriter, message string) {
	writeError(w, http.StatusNotImplemented, message)
}

// writeError 按协议要求的 JSON 格式返回错误码和提示信息。
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, zpay.ErrorResponse{Code: "error", Message: message})
}

// writeJSON 将指定数据作为 JSON 响应返回；调用方须确保其中没有密钥等敏感信息。
// any 是空接口的别名：type any = interface{}
/** 接口值携带“具体类型”+“具体值”，如果接口的值是nil但是携带具体类型，就不是空接口值。因此类型断言成功后，有时还要判断接口携带的指针值是否为空。
var response *zpay.ErrorResponse = nil
var data any = response
fmt.Println(response == nil) // true
fmt.Println(data == nil)     // false
*/
func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

// safetyHeaders 禁止缓存响应，限制请求体大小，并设置浏览器安全响应头。
// 这种“在原有处理器外面增加行为”的方式，通常叫作 HTTP 中间件，可以联系 Java 的 Filter、装饰器模式理解。
func safetyHeaders(next http.Handler) http.Handler {
	// HandlerFunc 是一个命名的函数类型：这个类型的值是函数，参数必须符合声明的签名，而且没有返回值
	// HandlerFunc 类型定义了 ServeHTTP(...) 方法，满足 http.Handler 接口
	// 这里实际上在做类型转换，把这里定义的匿名函数转换为 HandlerFunc
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		// no-referrer 会让浏览器普通表单的 Origin 变成 null，导致同源校验误拒绝。
		// 后台仅允许向同源发送 Referer；支付协议入口继续隐藏来源中的查询凭证。
		if strings.HasPrefix(r.URL.Path, "/admin/") {
			w.Header().Set("Referrer-Policy", "same-origin")
		}
		w.Header().Set("Content-Security-Policy", "default-src 'none'; img-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
		next.ServeHTTP(w, r)
	})
}
