// Package email 实现 gnotify 的 "email" 渠道，基于标准库 net/smtp，不依赖任何第三方邮件库。
//
// 注册：import 本包即触发 init() 注册 "email"。推荐用 [New] 携带 SMTP 配置构造。
//
// SMTP 配置作为 [New] 的第一个参数传入；Disabled/DryRun/日志/超时等通用能力
// 通过 gnotify.Option 注入。单测需要拦截网络时用 [NewWithDialer] 注入自定义拨号函数。
package email

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"sync"
	"time"

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

// addr 返回 "host:port"。
func (c EmailConfig) addr() string {
	port := c.Port
	if port <= 0 {
		if c.TLS {
			port = 465
		} else {
			port = 587
		}
	}
	return net.JoinHostPort(c.Host, strconv.Itoa(port))
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

	raw, err := n.buildMessage(msg)
	if err != nil {
		return err
	}

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(DefaultMailConcurrency)
	var mu sync.Mutex
	var errs []error
	for _, to := range msg.To {
		to := to
		to = strings.TrimSpace(to)
		if to == "" {
			mu.Lock()
			errs = append(errs, fmt.Errorf("%w: empty recipient", gnotify.ErrNoRecipient))
			mu.Unlock()
			continue
		}
		g.Go(func() error {
			if err := n.sendOne(gctx, to, raw); err != nil {
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
func (n *notifier) sendOne(ctx context.Context, to string, raw []byte) error {
	// 尊重 ctx 截止时间（在 dial 之前快速短路）。
	if err := ctx.Err(); err != nil {
		return err
	}

	conn, err := n.dialer("tcp", n.cfg.addr())
	if err != nil {
		return err
	}
	// 若 ctx 在会话中取消，关闭连接以尽快中断。正常结束时不触发。
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()

	if n.cfg.TLS {
		tlsConn := tls.Client(conn, &tls.Config{ServerName: n.cfg.Host})
		if err := tlsConn.Handshake(); err != nil {
			_ = conn.Close()
			return fmt.Errorf("tls handshake: %w", err)
		}
		conn = tlsConn
	}

	client, err := smtp.NewClient(conn, n.cfg.Host)
	if err != nil {
		_ = conn.Close()
		return err
	}
	defer func() { _ = client.Close() }()

	// 非隐式 TLS 时，若服务器支持则升级到 STARTTLS。
	if !n.cfg.TLS {
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(&tls.Config{ServerName: n.cfg.Host}); err != nil {
				return fmt.Errorf("starttls: %w", err)
			}
		}
	}

	if n.cfg.Username != "" || n.cfg.Password != "" {
		auth := smtp.PlainAuth("", n.cfg.Username, n.cfg.Password, n.cfg.Host)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("auth: %w", err)
		}
	}

	if err := client.Mail(n.cfg.From); err != nil {
		return fmt.Errorf("mail from: %w", err)
	}
	if err := client.Rcpt(to); err != nil {
		return fmt.Errorf("rcpt to: %w", err)
	}
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("data: %w", err)
	}
	if _, err := w.Write(raw); err != nil {
		_ = w.Close()
		return fmt.Errorf("write body: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("close body: %w", err)
	}
	return client.Quit()
}

// buildMessage 组装 MIME 邮件。中文标题用 mime.QEncoding 编码；
// 有 HTML 时发 multipart/alternative，否则纯文本 text/plain。
func (n *notifier) buildMessage(msg gnotify.Message) ([]byte, error) {
	var b strings.Builder

	writeHeader(&b, "From", n.cfg.From)
	writeHeader(&b, "To", strings.Join(msg.To, ", "))
	if msg.Subject != "" {
		writeHeader(&b, "Subject", mime.QEncoding.Encode("utf-8", msg.Subject))
	}
	writeHeader(&b, "MIME-Version", "1.0")
	writeHeader(&b, "Date", time.Now().Format(time.RFC1123Z))

	if msg.HTML == "" {
		writeHeader(&b, "Content-Type", "text/plain; charset=UTF-8")
		writeHeader(&b, "Content-Transfer-Encoding", "base64")
		b.WriteString("\r\n")
		b.WriteString(base64Lines([]byte(msg.Body)))
		return []byte(b.String()), nil
	}

	boundary := "gnotify-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	writeHeader(&b, "Content-Type", "multipart/alternative; boundary="+boundary)
	b.WriteString("\r\n")

	// 纯文本部分
	b.WriteString("--" + boundary + "\r\n")
	b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	b.WriteString("Content-Transfer-Encoding: base64\r\n\r\n")
	b.WriteString(base64Lines([]byte(msg.Body)))
	b.WriteString("\r\n")

	// HTML 部分
	b.WriteString("--" + boundary + "\r\n")
	b.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
	b.WriteString("Content-Transfer-Encoding: base64\r\n\r\n")
	b.WriteString(base64Lines([]byte(msg.HTML)))
	b.WriteString("\r\n")

	b.WriteString("--" + boundary + "--\r\n")
	return []byte(b.String()), nil
}

// writeHeader 写一行 header（含 CRLF）。
func writeHeader(b *strings.Builder, key, value string) {
	b.WriteString(key)
	b.WriteString(": ")
	b.WriteString(value)
	b.WriteString("\r\n")
}

// base64Lines 返回按 76 列换行、以 CRLF 分隔的 base64 文本，符合 MIME 编码约定。
func base64Lines(data []byte) string {
	enc := base64.StdEncoding.EncodeToString(data)
	var b strings.Builder
	for i := 0; i < len(enc); i += 76 {
		end := i + 76
		if end > len(enc) {
			end = len(enc)
		}
		b.WriteString(enc[i:end])
		b.WriteString("\r\n")
	}
	return b.String()
}
