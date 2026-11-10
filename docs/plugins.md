# 插件设计与 alipaycode 移植

## 插件对象

`plugin.Provider` 提供描述、结构配置校验和付款动作；`plugin.Poller` 提供账务查询。`Registry` 在启动时构建，拒绝重复 ID，运行时只读。

第一版插件随二进制编译发布，后台管理“启用哪些插件及如何配置账户”。不使用 Go 原生 .so，不运行上传的 PHP 源码。插件运行在主进程，是可信代码，接口不是安全隔离机制。

当前 alipaycode 的 `Describe().Implemented` 为 false，真实付款和账单查询返回 `ErrNotImplemented`。仅注册不会创建订单、访问支付宝或启动后台轮询。

## alipaycode 保留的能力

- 应用 APPID、支付宝 UID、应用私钥、支付宝公钥、可选 app_auth_token。
- 普通转账与转账确认单两种付款入口（normal/confirmation）。
- 付款入口附带精确金额和系统订单号备注，普通转账沿用“请勿添加备注-订单号”语义。
- 通过 `alipay.data.bill.accountlog.query` 查询到账，以备注、金额和有效入账信息匹配。

运行配置第一版草案为公钥模式。原项目还支持证书模式；配置 schema 尚未包括证书，正式移植时根据用户实际配置补充明确的互斥模式，不能假称已经兼容。

## 与核心的边界

- 核心先验证调用方并持久化订单，再传入 Checkout。PublicBaseURL 来自可信配置，不来自 Host 请求头。
- 插件生成结构化 PaymentAction；模板及二维码渲染由收银台负责，插件不能返回任意可执行 HTML。
- 核心负责窗口、分页、调度、限流、重试和事务；插件负责渠道协议、响应验签和字段映射。
- 插件可读取当前调用提供的配置，不获得任意 SQL 执行器，不直接更新订单或发送业务通知。
- 核心校验账户、有效入账、金额和备注，处理全局流水去重。配置账户标识与实际流水账户不一致时拒绝确认。
- 借助持久化窗口补查，避免照搬旧进程 `while(true)` 中的三分钟窗口和仅第一页读取。

## 原实现定位

参考仓库：`/Users/cbw/code/IdeaProjects/Epay`。

- `plugins/alipaycode/alipaycode_plugin.php`：通道配置、二维码入口、转账确认单。
- `plugins/alipaycode/inc/pay.page.php`：UID、金额、订单备注及支付宝唤起流程。
- `plugins/alipaycode/server.php`：账务查询和订单匹配。
- `includes/vendor/cccyun/alipay-sdk/src/AlipayBillService.php`：accountlog.query 请求字段。

只参考上述行为，不复制原项目自动加载代码或未经核验的 vendor。支付宝客户端将使用官方协议、可信密钥、TLS 校验和响应验签；不自行设计密码学算法。

## 后续扩展

新建独立插件包，实现必要接口，由构建流程自动发现并生成注册代码。正式增加回调型支付时，再引入受限的回调验证接口和规范化事件；不为目前不存在的支付方式预先创建一套空实现。

## 构建时自动发现

每个 `internal/plugin/<name>/` 直接子目录代表一个插件包。保留目录 `builtin`（生成的装配包）、`internal`（共享辅助包）、`testdata`，以及以 `.`、`_` 开头的目录不参与发现。插件目录不能是符号链接；插件内部的辅助子包不单独注册。

插件须提供一个无参数、无类型参数、单返回值的包级构造函数，例如：

```go
func New() *Plugin { return &Plugin{} }
```

返回类型必须实现 `plugin.Provider`，由生成代码的类型检查保证。构造函数只构造实例，不应连接数据库、发起网络请求或启动 goroutine。插件不能导入 `builtin`；它只依赖 `plugin` 契约及必要的领域类型。

`cmd/plugin-gen` 使用 `go list` 获取当前构建上下文选择的源码，检查构造函数，按导入路径排序并生成 `internal/plugin/builtin/registry_gen.go`。它不会执行插件代码；生成失败时保留原文件并让构建失败，不静默略过错误插件。插件 ID 来源于启动时的 `Describe()`，重复 ID 由 `plugin.NewRegistry` 明确拒绝。

```sh
make generate         # 自动发现并更新注册文件
make build            # 先生成，再编译服务
make run              # 先生成，再启动服务
make check            # 先生成，再执行静态检查与测试
make check-generated  # 只检查是否过期，不修改文件，适合 CI
```

新增或删除插件包后不必改 `app.New()`。生成文件纳入版本控制，不手工编辑，提交插件改动时一起提交；稳定排序、无时间戳、内容相同不写入，便于代码审查。普通 `go build` 不会自动运行生成器，会使用仓库中的现有注册文件；使用上述 make 入口，或先执行 `GOTOOLCHAIN=local go generate ./internal/plugin/builtin`。

当前插件约定使用可移植构造入口。若使用构建标签，生成和编译必须采用一致的 `GOFLAGS=-tags=...`。交叉编译需先为目标平台生成注册文件，再在同一目标上下文编译；不能以宿主平台生成结果假定覆盖所有平台特有插件。插件仍随主程序构建发布，运行时不扫描磁盘或加载外部代码。
