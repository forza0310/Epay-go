// 本文件集中定义后台模板；html/template 自动转义，已保存的敏感配置不回显。
package httpapi

import (
	"html/template"

	"epay/internal/channel"
)

type loginView struct{ CSRF, Error string }
type dashboardView struct {
	CSRF     string
	Channels []channel.Channel
}
type channelView struct {
	CSRF    string
	Channel channel.Channel
	Plugins []string
}

const head = `<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>epay 收款管理</title></head><body><main>`
const foot = `</main></body></html>`

var loginTemplate = template.Must(template.New("login").Parse(head + `<h1>管理员登录</h1>{{if .Error}}<p role="alert">{{.Error}}</p>{{end}}<form method="post" action="/admin/login"><input type="hidden" name="csrf_token" value="{{.CSRF}}"><p><label>密码 <input type="password" name="password" autocomplete="current-password" maxlength="1024" required></label></p><p><label>动态验证码 <input name="code" inputmode="numeric" pattern="[0-9]{6}" maxlength="6" autocomplete="one-time-code" required></label></p><button type="submit">登录</button></form>` + foot))
var dashboardTemplate = template.Must(template.New("dashboard").Parse(head + `<h1>收款通道</h1><p>当前可管理配置，尚未开放真实收款。</p><p><a href="/admin/channels/new">新增收款通道</a></p><table><thead><tr><th>名称</th><th>插件</th><th>账户标识</th><th>状态</th><th>操作</th></tr></thead><tbody>{{range .Channels}}<tr><td>{{.Name}}</td><td>{{.PluginID}}</td><td>{{.AccountID}}</td><td>{{if .Enabled}}已启用{{else}}已停用{{end}}</td><td><a href="/admin/channels/{{.ID}}">修改</a></td></tr>{{else}}<tr><td colspan="5">尚未配置收款通道</td></tr>{{end}}</tbody></table><form method="post" action="/admin/logout"><input type="hidden" name="csrf_token" value="{{.CSRF}}"><p><button type="submit">退出登录</button></p></form>` + foot))
var channelTemplate = template.Must(template.New("channel").Parse(head + `<h1>{{if .Channel.ID}}修改{{else}}新增{{end}}收款通道</h1><p><a href="/admin/">返回通道列表</a></p><form method="post" action="/admin/channels"><input type="hidden" name="csrf_token" value="{{.CSRF}}"><input type="hidden" name="id" value="{{.Channel.ID}}"><p><label>名称 <input name="name" value="{{.Channel.Name}}" maxlength="200" required></label></p><p><label>插件 <select name="plugin_id">{{range .Plugins}}<option value="{{.}}" {{if eq . $.Channel.PluginID}}selected{{end}}>{{.}}</option>{{end}}</select></label></p><p><label>账户标识 <input name="account_id" value="{{.Channel.AccountID}}" maxlength="200" required></label></p><p><label>状态 <select name="enabled"><option value="false" {{if not .Channel.Enabled}}selected{{end}}>停用</option><option value="true" {{if .Channel.Enabled}}selected{{end}}>启用</option></select></label></p><p><label>插件配置（JSON 对象）<br><textarea name="config" rows="12" cols="60" autocomplete="off" spellcheck="false" {{if not .Channel.ID}}required{{end}}></textarea></label></p><p>已保存的密钥不显示。修改时留空保留原配置；更换账户或插件须重新填写。当前保存不会验证支付宝连接。</p><button type="submit">保存</button></form>` + foot))

var setupTemplate = template.Must(template.New("setup").Parse(head + `<h1>首次初始化</h1><p>主密钥已由服务端生成或读取并保存至 data/master.key，无需手动配置。</p>{{if .Error}}<p role="alert">{{.Error}}</p>{{end}}<form method="post" action="/admin/setup"><input type="hidden" name="csrf_token" value="{{.CSRF}}"><p><label>管理员密码 <input type="password" name="password" autocomplete="new-password" maxlength="1024" required></label></p><p><label>确认密码 <input type="password" name="password_confirm" autocomplete="new-password" maxlength="1024" required></label></p><h2>绑定动态验证码</h2><p>使用密码管理器或验证器扫描二维码添加账户 epay / admin。</p><p><img src="/admin/setup/qr.png" width="256" height="256" alt="TOTP 动态验证码绑定二维码"></p><p>无法扫码时，使用下面的绑定密钥手工添加：基于时间、SHA-1、6 位、30 秒。</p><p>绑定密钥：<code>{{.Secret}}</code></p><p>请勿分享此绑定密钥。表单有效期为 10 分钟；过期或服务重启后请刷新页面并重新绑定。</p><p><label>当前动态码 <input name="code" inputmode="numeric" pattern="[0-9]{6}" maxlength="6" autocomplete="one-time-code" required></label></p><button type="submit">创建管理员</button></form>` + foot))
var setupCompleteTemplate = template.Must(template.New("setup-complete").Parse(head + `<h1>初始化完成</h1><p>管理员已创建，初始化入口已关闭。请等待验证器的下一个动态码，再登录后台。</p><p><a href="/admin/login">前往登录</a></p>` + foot))
