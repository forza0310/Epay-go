# ZPAY API 兼容对照

依据：[ZPAY 开发文档](https://z-pay.cn/doc.html#d1)，读取日期 2026-11-03。此表区分目标契约与当前实现，不能据此宣称已完成支付兼容。

## 接口范围

| 外部接口 | 目标行为 | 当前状态 |
|---|---|---|
| GET/POST `/submit.php` | 验签、幂等建单、跳转收银台 | 路由存在，HTTP 501 |
| POST `/mapi.php` | 建单并返回付款动作 | 路由存在，HTTP 501 |
| GET `/api.php?act=order` | 按系统单号或业务单号查询 | 路由存在，HTTP 501 |
| GET `/api.php?act=balance` | 不提供平台余额能力 | 路由存在，HTTP 501，返回错误 |
| 支付结果 `notify_url` | 服务端签名 GET 通知及重试 | 仅设计，未发送 |
| 完成跳转 `return_url` | 浏览器签名 GET 跳转 | 仅设计，未跳转 |
| POST `/api.php?act=refund` | 本阶段明确不支持退款 | HTTP 501 |
| ZPAY 微信小程序 | 属于 ZPAY 自有 appid/订单体系 | 不实现，不伪装兼容 |

`/healthz` 返回 200 仅代表进程存活；`/readyz` 保持 503。后台 `/admin/` 已开放，仅持有双因素认证会话的管理员可访问。

## 下单字段

| 字段 | submit.php | mapi.php | 设计说明 |
|---|---|---|---|
| pid | 必填 | 必填 | 单套 API 凭证标识，保留字符串，不创建商户账号 |
| type | 必填 | 必填 | 首版仅 alipay；未安装的方式明确拒绝 |
| out_trade_no | 必填 | 必填 | 文档声明数字、最多 32 位；保留字符串防止精度丢失 |
| name | 必填 | 必填 | 商品名，按通知文档不超过 100 个字符 |
| money | 必填 | 必填 | 正金额，最多两位小数；内部整数分 |
| notify_url | 必填 | 必填 | 无查询参数；必须与管理员预登记地址匹配 |
| return_url | 必填 | 文档未要求 | API 支付可缺省，扩展传入则参与验签并校验 |
| clientip | 文档未要求 | 必填 | 合法 IP；只是业务字段，不能用于管理员鉴权或限流来源 |
| cid | 可选 | 可选 | 逗号分隔候选通道；不存在/停用/类型不匹配时拒绝 |
| param | 可选 | 可选 | 原样保存、通知原样返回，不裁剪/重编码后再验签 |
| device | 文档未要求 | 可选 | 默认为 pc |
| sign | 必填 | 必填 | 小写 MD5 十六进制签名 |
| sign_type | 必填 | 必填 | 按文档必填字段，严格接受 MD5 |

POST 同时支持 `multipart/form-data`（文档要求）及 `application/x-www-form-urlencoded`（常见客户端兼容）。拒绝文件上传、重复参数及在 URL 与 POST 中重复提供业务字段。表单解析前限制请求体；验签前只进行一次标准 URL/form 解码。

支付接口的输入校验、请求鉴权和以上表单解析尚未接入 HTTP 层。当前支付入口返回未实现或参数错误，不接收真实订单。

## 响应约定与文档差异

- API 支付成功：`code: 1`，返回 `msg`、`O_id`、`trade_no` 和可用的 `payurl`/`qrcode`/`img`。
- `O_id` 使用本系统订单号，不代表已在 ZPAY 创建订单。不会跳转 ZPAY 专属微信小程序。
- 支付宝插件不提供 `payurl2` 微信 H5 能力，缺省省略。只有真正提供二维码图片接口时才返回 `img`，不能返回一个不存在的资源。
- 文档失败示例使用 `{"code":"error","msg":"..."}`，而表格描述 code 为 Int；当前按失败示例建模，避免使用 `any` 混合正常响应。
- 文档下单的 pid 为字母数字 String，但查询返回表写 Int。设计统一返回字符串，保护长 ID 和非数字 PID；与强制数字 PID 的旧客户端存在差异，联调时确认。
- 订单查询状态：已支付映射 `status=1`；待支付、过期、关闭映射 `0`。`buyer` 缺省为空，`param` 原样返回。
- 两个查询订单号至少传一个；同时传入时必须指向同一订单，不能忽略冲突参数。
- `act=order` 将按文档使用 `pid/key` 验证；`act=balance` 直接返回不支持错误。HTTPS 必须启用；应用及代理访问日志均须屏蔽 key 和完整查询字符串。

## 余额接口

ZPAY 文档定义的是“ZPAY 平台账户余额”。本系统的款项直接进入个人支付宝，没有平台余额。

本系统不托管 ZPAY 平台余额，`act=balance` 不实现；调用时返回 HTTP 501 和错误消息 `act=balance 暂不支持`。不能用累计到账金额冒充余额，也不调用支付宝余额接口替代该协议语义。

## 签名（已实现）

1. 所有参数按参数名 ASCII 升序排列；仅排除 `sign`、`sign_type`、空字符串。
2. `0` 不属于空值，必须参与签名。
3. 使用 `key=value`、`&` 连接，值不再 URL 编码，最后直接附加密钥。
4. 对 UTF-8 字节计算 MD5，输出小写十六进制；使用固定长度恒定时间比较验签。
5. 拒绝重复参数，sign_type 必须为 MD5。MD5 仅用于既定协议兼容。

## 通知（待实现）

GET 参数：`pid,name,money,out_trade_no,trade_no,param,trade_status,type,sign,sign_type`。
只有 `TRADE_SUCCESS` 表示成功。签名时使用未 URL 编码的值，发送时再统一编码。

单次请求超时 5 秒。按文档的相邻重试间隔 `0/15/15/30/180/1800/1800/1800/1800/3600` 秒调度。兼容文档约定：HTTP 成功响应的有限长度响应体包含 `success` 时确认送达；否则继续重试。达到上限后标记 exhausted，供管理员查看。

通知目标必须预登记，生产使用 HTTPS，DNS/IP 和重定向限制见安全文档。浏览器 return_url 与服务端 notify_url 使用各自的地址策略，不能混用信任边界。

## 管理后台（本系统自有接口）

| 路由 | 行为 | 保护 |
|---|---|---|
| GET `/admin/setup` | 首次初始化表单 | 仅管理员尚未创建时开放 |
| POST `/admin/setup` | 设置密码并验证 TOTP 绑定 | 初始化 Cookie、表单 CSRF、来源检查、唯一管理员约束 |
| GET `/admin/setup/qr.png` | TOTP 绑定二维码 | 有效初始化 Cookie、同源访问、不缓存；管理员创建后关闭 |
| GET/POST `/admin/login` | 登录表单 / 密码与动态码登录 | 签名双提交 CSRF、来源检查、持久化限流 |
| GET `/admin/` | 通道列表 | 双因素会话 |
| GET `/admin/channels/new` | 新建表单 | 双因素会话 |
| GET `/admin/channels/{id}` | 编辑元数据，配置不回显 | 双因素会话 |
| POST `/admin/channels` | 新建或修改/启停通道 | 会话、会话绑定 CSRF、来源检查 |
| POST `/admin/logout` | 撤销当前会话 | 会话、会话绑定 CSRF |

后台 POST 仅接受 `application/x-www-form-urlencoded`，禁止查询参数、重复字段和未知字段；请求体上限 64 KiB，插件 JSON 上限 32 KiB。业务失败不回显请求配置。密码错误与动态码错误合并提示；限流返回 429，页面单独显示本次请求时需等待的分钟/秒数，Retry-After 与页面时长一致，等待后须刷新登录页获取新表单；未登录管理页跳转到登录页。这些接口不属于 ZPAY 协议，不接受 PID/key 作为管理员凭证。
