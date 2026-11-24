package nats

import (
	"crypto/tls"
	"time"
)

// Config 是 NATS 客户端的连接与行为配置。
//
// 参照切法 A：本包忠实暴露 NATS 语义，Config 只放 NATS 自身需要的连接参数；
// 跨引擎一致的日志/callerSkip 由 mqaccess.Option 承载，不塞进这里。
type Config struct {
	// Service 服务名，必填，落日志 service 字段；同时作为默认的 NATS Name。
	Service string

	// URL 是 NATS 服务端地址，如 nats://127.0.0.1:4222；可逗号分隔多个做故障转移。
	URL string

	// Name 是客户端名称（nats.Name），便于服务端监控/排障；为空时回退到 Service。
	Name string

	// JetStream 标记是否启用 JetStream。本期 core publish/subscribe 优先，
	// 置 true 暂不改变连接行为（预留开关，见 nats.go 中的说明）。
	JetStream bool

	// ConnectTimeout 是建连超时；<=0 时用默认值（defaultConnectTimeout）。
	ConnectTimeout time.Duration

	// --- 可选认证，按需填写其中一组 ---

	// User / Password 用于基本用户名密码认证。
	User     string
	Password string

	// Token 用于 Token 认证。
	Token string

	// NKeySeed 是 NKey 种子串（可选，需 nats.NkeyOptionFromSeed 解析）。
	NKeySeed string

	// TLSConfig 自定义 TLS 配置；为 nil 时不启用 TLS。
	TLSConfig *tls.Config
}

const (
	// defaultConnectTimeout 是未显式配置 ConnectTimeout 时的建连超时。
	defaultConnectTimeout = 5 * time.Second
	// defaultName 是 Name 与 Service 都为空时的兜底客户端名（正常情况下 Service 已校验非空）。
	defaultName = "golib-mqaccess-nats"
)
