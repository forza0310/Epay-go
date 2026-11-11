# epay

单管理员、自用收款、可扩展插件的 Go 服务。当前交付为 **框架、持久化与管理员服务基础版**，首个插件为 `alipaycode`；兼容接口以 [ZPAY 文档](https://z-pay.cn/doc.html#d1) 为依据。

## 当前状态

- 已实现：进程入口、配置读取、HTTP 服务与优雅退出、健康检查、插件注册、协议数据结构、MD5 签名/验签、整数金额解析、SQLite 迁移与仓储、通道配置 AES-GCM 加密及主密钥校验、管理员初始化、密码与 TOTP 验证和会话服务。
- 已定义：订单/到账模型、原子持久化契约、alipaycode 插件边界。
- 未实现：管理员 HTTP 鉴权与页面、真实下单/付款页面、支付宝调用、账单调度、订单查询及通知投递。
- 支付与管理路由返回 **HTTP 501**，`/readyz` 返回 **503**。目前不能用于真实收款。没有默认管理员、虚构订单或模拟成功返回。

## 本地运行

按用户指定使用 **Go 1.27.3**。SQLite 使用纯 Go 驱动 `modernc.org/sqlite v1.62.0`，依赖固定在 `go.mod`/`go.sum`。

持久化、加密和管理员服务以独立模块提供，启动装配及 HTTP 接入另行交付；当前启动服务仍不提供真实收款或管理员登录。迁移脚本位于 `internal/storage/sqlite/migrations/`，随程序编译，不再维护独立 `schema.sql`。

```sh
GOTOOLCHAIN=local go run ./cmd/epay
# 或：make run
```

默认监听 `127.0.0.1:8080`；设置 `EPAY_ADDR` 可调整地址。程序自动读取启动工作目录的 `.env`，可参考 `.env.example` 创建配置文件。`.env` 已被 Git 忽略，包含敏感配置时应使用 `chmod 600 .env` 限制权限。

配置优先级为 **已有进程环境变量（包括 IDEA 运行配置）→ `.env` → 默认值**。显式设置的空环境变量也不会被文件覆盖；若希望使用 `.env` 中的值，请删除 IDEA 中对应的环境变量条目。

只读取启动工作目录下的 `.env`，不向父目录查找，不依赖 `go.mod` 或可执行文件位置。IDEA 的 Working directory 应设置为存放 `.env` 的目录，例如项目根目录。加载不改写进程全局环境，修改配置后需要重启程序。

支持 `NAME=value`、可选 `export` 前缀、空行、注释、单行单双引号；双引号可使用 `\n`、`\t`、`\"`、`\\` 等转义。未加引号的行尾注释须与值用空白隔开。支持 UTF-8 BOM 和 CRLF；不支持多行值、变量插值或 shell 命令执行，例如 `$(cat ...)` 会被当作普通文本。文件上限 64 KiB，重复变量和语法错误会阻止启动，错误仅报告行号和原因，不输出变量值。文件不存在时仍可完全通过环境变量运行。

```sh
curl http://127.0.0.1:8080/healthz
# HTTP 200: {"stage":"scaffold","status":"ok"}
curl http://127.0.0.1:8080/readyz
# HTTP 503: 未具备业务就绪条件
curl 'http://127.0.0.1:8080/api.php?act=order'
# HTTP 501: {"code":"error","msg":"订单查询尚未实现"}
```

## 构建与检查

```sh
make build    # 自动发现插件，再构建 bin/epay
make check    # 自动发现插件，再执行 go vet ./... && go test ./...
```

Makefile 显式禁用工具链自动切换。直接使用 Go 命令时同样设置 `GOTOOLCHAIN=local`。

测试覆盖签名、金额、插件发现、配置加载、SQLite 迁移与重启、限流持久化、密文完整性及管理员双因素与会话生命周期。具体结果见 [验证记录](docs/verification.md)。

插件通过构建时自动发现注册。新增或删除 `internal/plugin/<name>/` 插件包后执行 `make build`，无需修改启动代码。生成文件须一并提交；`make check-generated` 可检查是否过期。直接使用 `go build` 前须执行 `make generate`，详细约定见 [插件设计](docs/plugins.md)。

管理员服务的职责、安全规则与验证范围见 [管理员服务](docs/admin.md)。

## 阅读顺序

1. [智能体工程规范与自动发现入口](AGENTS.md)。
2. [架构与数据流程](docs/architecture.md)。
3. [API 兼容对照](docs/api-compatibility.md)。
4. [插件契约与 alipaycode](docs/plugins.md)。
5. [关键设计决策](docs/decisions.md)。
6. [安全与上线条件](docs/security.md)。

请重点确认：单人直收下余额接口的定义；API 格式兼容范围。确认后再按架构文档的阶段实现业务。
