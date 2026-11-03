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

新建独立插件包，实现必要接口，在组合根注册。正式增加回调型支付时，再引入受限的回调验证接口和规范化事件；不为目前不存在的支付方式预先创建一套空实现。
