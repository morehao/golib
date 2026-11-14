# gnotify — 消息通知

`gnotify` 提供统一的消息通知能力，首期与二期共支持四个渠道：邮件、企业微信、短信（阿里云/腾讯云）、微信公众号模板消息。

- **接口 + 渠道驱动注册**：`Notifier` 接口定在根包，各渠道 `init()` 注册（对齐 `storage`/`gllm`），用 `blank import` 触发。
- **凭据由调用方注入**：各渠道的凭据通过渠道级 `New(cfg)` 传入，**不隐式读 DB/env/settings**（可测试、可移植）。
- **零 SDK 优先**：纯标准库 + 注入的 `http.Client`（邮件用 `net/smtp`），不引云厂商 SDK。
- **不落库、不内部重试、不做验证码校验**：`Send` 返回 error 由调用方决定重试；通知是「发出去就算」的动作。

## 快速开始

```go
import (
    "context"
    "github.com/morehao/golib/gnotify"
)

ctx := context.Background()

// 通过注册表按渠道构造（需对应渠道包的 blank import）
n, err := gnotify.New("email") // 未注册 → gnotify.ErrChannelNotRegistered
_ = n
```

> 推荐直接用**渠道级构造函数**注入凭据（见下），`gnotify.New(channel)` 适合从配置中心拿渠道名再分发的场景。

## 渠道

### 邮件（`gnotify/email`）

标准库 `net/smtp`，支持纯文本 + HTML（multipart/alternative）、中文标题（MIME 编码）、隐式 TLS(465)/STARTTLS(587)。

```go
import _ "github.com/morehao/golib/gnotify/email"

n, err := email.New(email.EmailConfig{
    Host: "smtp.example.com", Port: 587,
    Username: "no-reply@example.com", Password: "***",
    From: "no-reply@example.com", TLS: false,
})
err = n.Send(ctx, gnotify.Message{
    To:      []string{"user@example.com"},
    Subject: "标题",
    Body:    "纯文本正文",
    HTML:    "<b>HTML 正文</b>",
})
```

### 企业微信（`gnotify/wecom`）

群机器人 webhook，`text`/`markdown`（`Params["msgtype"]` 指定，默认 markdown）。

```go
import _ "github.com/morehao/golib/gnotify/wecom"

n, _ := wecom.New(wecom.WecomConfig{WebhookURL: "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=xxx"})
_ = n.Send(ctx, gnotify.Message{Body: "**告警**：磁盘 90%", Params: map[string]string{"msgtype": "markdown"}})
```

### 短信（`gnotify/sms/aliyun`、`gnotify/sms/tencent`）

`Message.To` 为手机号，`Params` 为模板变量。

```go
import _ "github.com/morehao/golib/gnotify/sms/aliyun"

n, _ := aliyun.New(aliyun.AliyunConfig{
    AccessKeyID: "AK", AccessKeySecret: "SK",
    SignName: "签名", TemplateCode: "SMS_123456789", RegionID: "cn-hangzhou",
})
_ = n.Send(ctx, gnotify.Message{
    To:     []string{"13800000000"},
    Params: map[string]string{"code": "1234"},
})
```

```go
import _ "github.com/morehao/golib/gnotify/sms/tencent"

n, _ := tencent.New(tencent.TencentConfig{
    SecretID: "ID", SecretKey: "KEY", SdkAppID: "1400000000",
    SignName: "签名", TemplateID: "123456", Region: "ap-guangzhou",
})
_ = n.Send(ctx, gnotify.Message{
    To:     []string{"13800000000"}, // 自动补 +86
    Params: map[string]string{"param": "1234"}, // 或 params="a,b,c"
})
```

### 微信公众号模板消息（`gnotify/wechatmp`）

`Message.To[0]` 为 openid，`Params` 为模板变量；`appid/secret` 换 access_token（渠道内部 TTL 内存缓存 + 失效重取）。

```go
import _ "github.com/morehao/golib/gnotify/wechatmp"

n, _ := wechatmp.New(wechatmp.WechatMPConfig{AppID: "wx...", Secret: "...", TemplateID: "tpl..."})
_ = n.Send(ctx, gnotify.Message{
    To:     []string{"openid"},
    Params: map[string]string{"first": "您好", "keyword1": "订单", "url": "https://..."},
})
```

## 路由（显式渠道，不隐式广播）

```go
r := gnotify.NewRouter(mail, wecomN)
err := r.Send(ctx, "email", msg) // 只投递给指定渠道
```

## 配置与选项

`Config{Logger glog.Logger; HTTPClient *http.Client; Timeout}`，函数式选项：`WithLogger`/`WithHTTPClient`/`WithTimeout`。渠道级 `NewWithCore(cfg, ...gnotify.Option)` 可同时注入 http.Client/超时。

## 注册表

| 渠道 | 注册名 | blank import |
|---|---|---|
| 邮件 | `email` | `_ "github.com/morehao/golib/gnotify/email"` |
| 企业微信 | `wecom` | `_ "github.com/morehao/golib/gnotify/wecom"` |
| 阿里云短信 | `aliyun` | `_ "github.com/morehao/golib/gnotify/sms/aliyun"` |
| 腾讯云短信 | `tencent` | `_ "github.com/morehao/golib/gnotify/sms/tencent"` |
| 公众号模板消息 | `wechatmp` | `_ "github.com/morehao/golib/gnotify/wechatmp"` |

## 错误分层

`gnotify:` 前缀：`ErrInvalidConfig`、`ErrChannelNotRegistered`、`ErrNoRecipient`、`ErrSendFailed`。渠道把底层错误包在 `ErrSendFailed` 之上，`errors.Is` 可命中。
