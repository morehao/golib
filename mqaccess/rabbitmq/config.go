package rabbitmq

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net/url"
	"time"
)

// Config 是 RabbitMQ 引擎的专属配置。
//
// 跨引擎一致的配置（日志、callerSkip）走 mqaccess.Option，不放在这里。
// 这里只放 RabbitMQ 自身的连接 / 认证 / 超时参数。
type Config struct {
	// Service 服务名，必填；落日志 service 字段，也用于日志身份区分。
	Service string

	// URL 是 AMQP 连接串，如 amqp://admin:admin123@127.0.0.1:5672/（vhost 默认 /）。
	// 与 Host/Port/User/Password/Vhost 二选一：URL 非空时优先使用 URL。
	URL string

	// --- 当 URL 为空时，用下列字段拼 AMQP URI（全可选，缺省用 amqp 默认值） ---

	// Host 及 Port 为 broker 地址；Port<=0 时用 5672。
	Host string
	Port int
	// User / Password 为 AMQP 认证信息。
	User     string
	Password string
	// Vhost 为虚拟主机；为空时用默认 vhost "/"。
	Vhost string

	// TLSConfig 非空时以 amqps 拨号并启用 TLS。
	TLSConfig *tls.Config

	// DialTimeout 是建连超时；<=0 时用默认值（defaultDialTimeout）。
	DialTimeout time.Duration

	// Heartbeat 是 AMQP 心跳间隔；<=0 时用默认值（defaultHeartbeat）。
	Heartbeat time.Duration
}

const (
	// defaultDialTimeout 是未显式配置 DialTimeout 时的建连超时。
	defaultDialTimeout = 5 * time.Second
	// defaultHeartbeat 是未显式配置 Heartbeat 时的心跳间隔。
	defaultHeartbeat = 10 * time.Second
	// defaultPort 是未显式配置 Port 时的 AMQP 端口。
	defaultPort = 5672
)

// validate 校验必填项：Service 必填，且必须有 URL 或 Host 之一。
func (c *Config) validate() error {
	if c == nil {
		return errors.New("rabbitmq: config is nil")
	}
	if c.Service == "" {
		return errors.New("rabbitmq: Service is required")
	}
	if c.URL == "" && c.Host == "" {
		return errors.New("rabbitmq: URL or Host is required")
	}
	return nil
}

// dialTimeout 返回生效的建连超时。
func (c *Config) dialTimeout() time.Duration {
	if c.DialTimeout > 0 {
		return c.DialTimeout
	}
	return defaultDialTimeout
}

// heartbeat 返回生效的心跳间隔。
func (c *Config) heartbeat() time.Duration {
	if c.Heartbeat > 0 {
		return c.Heartbeat
	}
	return defaultHeartbeat
}

// port 返回生效的端口。
func (c *Config) port() int {
	if c.Port > 0 {
		return c.Port
	}
	return defaultPort
}

// addressForLog 返回落日志的 server.address：脱敏后的 broker 地址（不含密码）。
func (c *Config) addressForLog() string {
	if c.URL != "" {
		return redactURI(c.URL)
	}
	scheme := "amqp"
	if c.TLSConfig != nil {
		scheme = "amqps"
	}
	return fmt.Sprintf("%s://%s:%d", scheme, c.Host, c.port())
}

// buildURI 按 Host/Port/User/Password/Vhost 拼 AMQP URI（URL 为空时使用）。
func (c *Config) buildURI() string {
	scheme := "amqp"
	if c.TLSConfig != nil {
		scheme = "amqps"
	}
	auth := ""
	if c.User != "" || c.Password != "" {
		auth = url.UserPassword(c.User, c.Password).String() + "@"
	}
	vhost := c.Vhost
	if vhost == "" {
		vhost = "/"
	}
	return fmt.Sprintf("%s://%s%s:%d/%s", scheme, auth, c.Host, c.port(), url.PathEscape(vhost))
}

// redactURI 把 AMQP URI 里的密码替换为 ***，避免日志泄露凭据。
func redactURI(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if u.User != nil {
		if _, hasPwd := u.User.Password(); hasPwd {
			u.User = url.UserPassword(u.User.Username(), "***")
		}
	}
	return u.String()
}
