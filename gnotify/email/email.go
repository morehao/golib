// Package email 实现 gnotify 的 "email" 渠道，内部基于 github.com/wneessen/go-mail。
//
// 注册：import 本包即触发 init() 注册 "email"。推荐用 [New] 携带 SMTP 配置构造。
//
// SMTP 配置作为 [New] 的第一个参数传入；Disabled/DryRun/日志/超时等通用能力
// 通过 gnotify.Option 注入。单测需要拦截网络时用 [NewWithDialer] 注入自定义拨号函数。
package email

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/wneessen/go-mail"
	"golang.org/x/sync/errgroup"

	"github.com/morehao/golib/gnotify"
)

// DefaultMailConcurrency 是 [notifier.Send] 逐收件人并发投递时的默认最大并发数。
const DefaultMailConcurrency = 4

func init() {
	gnotify.Register("email", newNotifier)
}

// EmailConfig 是 SMTP 连接与发件人配置。
type EmailConfig struct {
	// Host 是 SMTP 服务器地址，必填。
	Host string

	// Port 是 SMTP 端口。常见：25（明文）、465（隐式 TLS）、587（STARTTLS）。
	// <=0 时按 TLS 标志取默认：TLS=true 用 465，否则 587。
	Port int

	// Username / Password 是 SMTP 鉴权凭据。两者都为空时跳过 AUTH。
	Username string
	Password string

	// From 是发件人地址，必填。
	From string

	// TLS 为 true 时使用隐式 TLS（SMTPS，通常 465 端口）。
	// 为 false 时用明文连接，若服务器支持则尝试 STARTTLS。
	TLS bool
}

// validate 校验必填字段，返回 [gnotify.ErrInvalidConfig] 包装的错误。
func (c EmailConfig) validate() error {
	if strings.TrimSpace(c.Host) == "" {
		return fmt.Errorf("%w: email host is empty", gnotify.ErrInvalidConfig)
	}
	if strings.TrimSpace(c.From) == "" {
		return fmt.Errorf("%w: email from is empty", gnotify.ErrInvalidConfig)
	}
	return nil
}

// port 返回实际使用的端口（<=0 时按 TLS 标志取默认值）。
func (c EmailConfig) port() int {
	if c.Port > 0 {
		return c.Port
	}
	if c.TLS {
		return 465
	}
	return 587
}

// addr 返回 "host:port"。
func (c EmailConfig) addr() string {
	return net.JoinHostPort(c.Host, fmt.Sprintf("%d", c.port()))
}

// dialFunc 建立到底层 SMTP 服务器的连接。默认 net.Dial；单测可注入以拦截网络。
type dialFunc func(network, addr string) (net.Conn, error)

// notifier 是 "email" 渠道的 [gnotify.Notifier] 实现。
type notifier struct {
	cfg    EmailConfig
	core   gnotify.Config
	dialer dialFunc
}

// newNotifier 是注册进 gnotify 的 Factory，用于让 "email" 出现在注册表里
// （使未注册渠道能被 ErrChannelNotRegistered 区分）。
//
// 通过统一的 gnotify.New("email") 构造时不带渠道凭据，此时 notifier 的
// EmailConfig 为空，Send 会返回 ErrInvalidConfig。携带 SMTP 配置请用 [New]。
func newNotifier(core gnotify.Config) (gnotify.Notifier, error) {
	return gnotify.Guard(&notifier{core: core, dialer: net.Dial}, core), nil
}

// New 直接构造 email Notifier，携带 SMTP 配置，并可通过 gnotify.Option 注入
// 日志、Disabled（整体静音）、DryRun（干跑）等通用能力。
//
//	n, err := email.New(email.EmailConfig{Host: "...", From: "a@x.com"})
//
// 构造时校验必填字段（host/from）；缺失返回 [gnotify.ErrInvalidConfig]。
func New(cfg EmailConfig, opts ...gnotify.Option) (gnotify.Notifier, error) {
	return newWithDialer(cfg, net.Dial, opts...)
}

// NewWithDialer 在 [New] 基础上注入自定义拨号函数，主要用于单测拦截 SMTP 网络。
// 生产代码通常用 [New] 即可。
func NewWithDialer(cfg EmailConfig, d func(network, addr string) (net.Conn, error), opts ...gnotify.Option) (gnotify.Notifier, error) {
	if d == nil {
		d = net.Dial
	}
	return newWithDialer(cfg, d, opts...)
}

func newWithDialer(cfg EmailConfig, d dialFunc, opts ...gnotify.Option) (gnotify.Notifier, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	core := gnotify.ApplyOptions(opts...)
	n := &notifier{cfg: cfg, core: core, dialer: d}
	return gnotify.Guard(n, core), nil
}

// Channel 实现 [gnotify.Notifier]。
func (n *notifier) Channel() string { return "email" }

// Send 逐收件人并发送邮件，失败用 errors.Join 聚合。
//
// 每个收件人单独建立一次 SMTP 会话（简单、隔离失败），避免一个坏地址影响其他人。
// 并发度受 [DefaultMailConcurrency] 限制；单个收件人失败不会中断其它收件人的投递。
func (n *notifier) Send(ctx context.Context, msg gnotify.Message) error {
	if err := n.cfg.validate(); err != nil {
		return err
	}
	if len(msg.To) == 0 {
		return gnotify.ErrNoRecipient
	}

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(DefaultMailConcurrency)
	var mu sync.Mutex
	var errs []error
	for _, to := range msg.To {
		to = strings.TrimSpace(to)
		if to == "" {
			mu.Lock()
			errs = append(errs, fmt.Errorf("%w: empty recipient", gnotify.ErrNoRecipient))
			mu.Unlock()
			continue
		}
		g.Go(func() error {
			if err := n.sendOne(gctx, to, msg); err != nil {
				mu.Lock()
				errs = append(errs, fmt.Errorf("%w: to %s: %v", gnotify.ErrSendFailed, to, err))
				mu.Unlock()
				if n.core.Logger != nil {
					n.core.Logger.Errorw(ctx, "gnotify/email: send failed", "to", to, "err", err)
				}
			}
			return nil // 单失败不 return err，保持隔离
		})
	}
	_ = g.Wait()
	return errors.Join(errs...)
}

// sendOne 对一个收件人完成一次 SMTP 会话。
func (n *notifier) sendOne(ctx context.Context, to string, msg gnotify.Message) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	m, err := n.buildMsg(to, msg)
	if err != nil {
		return err
	}

	client, err := n.newClient()
	if err != nil {
		return err
	}
	if err := client.DialAndSendWithContext(ctx, m); err != nil {
		return err
	}
	return nil
}

// newClient 依据配置构造 go-mail Client。
//
// TLS 语义对齐：
//   - TLS=true：隐式 TLS（SMTPS），WithSSL + 显式端口（默认 465）。
//   - TLS=false：明文连接；若服务器宣告支持则升级 STARTTLS（TLSOpportunistic）。
//   - Username/Password 任一非空时才启用 AUTH（PLAIN）。
//   - 自定义 dialer（单测注入）通过 WithDialContextFunc 适配。
func (n *notifier) newClient() (*mail.Client, error) {
	opts := []mail.Option{
		mail.WithPort(n.cfg.port()),
		mail.WithTimeout(30 * time.Second),
	}
	if n.cfg.TLS {
		// 隐式 TLS：go-mail 用 tls.Dialer 建立连接（默认端口 465，已由 WithPort 显式指定）。
		opts = append(opts, mail.WithSSL())
	} else {
		// 明文连接，服务器支持 STARTTLS 时升级；不支持则退回明文。
		// 用 WithTLSPolicy（而非 WithTLSPortPolicy）避免其自动改写端口/回落到 25。
		opts = append(opts, mail.WithTLSPolicy(mail.TLSOpportunistic))
	}
	if n.cfg.Username != "" || n.cfg.Password != "" {
		opts = append(opts,
			mail.WithSMTPAuth(mail.SMTPAuthPlain),
			mail.WithUsername(n.cfg.Username),
			mail.WithPassword(n.cfg.Password),
		)
	}
	// 自定义拨号函数适配为 go-mail 的 DialContextFunc。
	if n.dialer != nil {
		d := n.dialer
		opts = append(opts, mail.WithDialContextFunc(func(_ context.Context, network, _ string) (net.Conn, error) {
			return d(network, n.cfg.addr())
		}))
	}
	return mail.NewClient(n.cfg.Host, opts...)
}

// buildMsg 把渠道无关的 gnotify.Message 映射为 go-mail 的 *mail.Msg。
//
// 中文主题由 go-mail 自动做 MIME 编码；纯文本落到 BodyString，HTML 作为
// multipart/alternative 的 text/html 备选部分。
func (n *notifier) buildMsg(to string, msg gnotify.Message) (*mail.Msg, error) {
	m := mail.NewMsg()
	if err := m.From(n.cfg.From); err != nil {
		return nil, fmt.Errorf("set from: %w", err)
	}
	if err := m.To(to); err != nil {
		return nil, fmt.Errorf("set to: %w", err)
	}
	if msg.Subject != "" {
		m.Subject(msg.Subject)
	}
	m.SetBodyString(mail.TypeTextPlain, msg.Body)
	if msg.HTML != "" {
		m.AddAlternativeString(mail.TypeTextHTML, msg.HTML)
	}
	return m, nil
}
