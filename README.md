# epay

单管理员、自用收款、可扩展插件的 Go 服务。当前交付为 **框架评审版**，首个插件为 `alipaycode`；兼容接口以 [ZPAY 文档](https://z-pay.cn/doc.html#d1) 为依据。

## 当前状态

- 已实现：进程入口、配置读取、HTTP 服务与优雅退出、健康检查、插件注册、协议数据结构、MD5 签名/验签和整数金额解析。
- 已定义：订单/到账模型、原子持久化契约、alipaycode 插件边界、SQLite 表结构草案。
- 未实现：管理员鉴权与页面、持久化适配器、真实下单/付款页面、支付宝调用、账单调度、订单查询及通知投递。
- 支付与管理路由返回 **HTTP 501**，`/readyz` 返回 **503**。目前不能用于真实收款。没有默认管理员、虚构订单或模拟成功返回。

## 本地运行

按用户指定使用 **Go 1.26.4**。项目没有第三方 Go 依赖，无需运行 `go mod tidy`，也没有需要提交的 `go.sum`。

```sh
GOTOOLCHAIN=local go run ./cmd/epay
# 或：make run
```

默认监听 `127.0.0.1:8080`；设置 `EPAY_ADDR` 可调整地址。`.env.example` 仅展示环境变量，程序不会自动读取 `.env`。

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
make build    # bin/epay
make check    # go vet ./... && go test ./...
```

Makefile 显式禁用工具链自动切换。直接使用 Go 命令时同样设置 `GOTOOLCHAIN=local`。

仅为签名和金额边界保留两个测试文件；路由及 SQL 草案通过定向冒烟和内存数据库检查验证。具体结果见 [验证记录](docs/verification.md)。

## 阅读顺序

1. [智能体工程规范与自动发现入口](AGENTS.md)。
2. [架构与数据流程](docs/architecture.md)。
3. [API 兼容对照](docs/api-compatibility.md)。
4. [插件契约与 alipaycode](docs/plugins.md)。
5. [关键设计决策](docs/decisions.md)。
6. [安全与上线条件](docs/security.md)。

请重点确认：插件随程序发布的方式；单人直收下余额接口的定义；API 格式兼容范围。确认后再按架构文档的阶段实现业务。
