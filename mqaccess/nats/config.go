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
	// 留空时由 nats.Connect 使用其默认地址（127.0.0.1:4222）。
	URL string

	// Name 是客户端名称（nats.Name），便于服务端监控/排障；为空时回退到 Service。
	Name string

	// JetStream 标记是否启用 JetStream。为 true 时 New 会基于已建好的连接
	// 初始化 JetStream 上下文（可通过 Client.JS() 取得），并暴露 EnsureStream/
	// EnsureConsumer/PublishJS/SubscribeJS 等 JetStream API（见 jetstream.go）。
	// 为 false 时连接与 core publish/subscribe 行为完全不变。
	JetStream bool

	// ConnectTimeout 是建连超时；<=0 时用默认值（defaultConnectTimeout）。
	ConnectTimeout time.Duration

	// --- 可选认证，按需填写其中一组 ---

	// User / Password 用于基本用户名密码认证。
	User     string
	Password string

	// Token 用于 Token 认证。
	Token string

	// NKeySeedFile 是 NKey 种子**文件路径**（注意不是种子内容）。
	// 底层 nats.NkeyOptionFromSeed 按路径读取种子文件，传入种子串会读取失败。
	NKeySeedFile string

	// TLSConfig 自定义 TLS 配置；为 nil 时不启用 TLS。
	TLSConfig *tls.Config
}

const (
	// defaultConnectTimeout 是未显式配置 ConnectTimeout 时的建连超时。
	defaultConnectTimeout = 5 * time.Second
	// defaultName 是 Name 与 Service 都为空时的兜底客户端名（正常情况下 Service 已校验非空）。
	defaultName = "golib-mqaccess-nats"
	// defaultDrainTimeout 是 Close 等待 Drain 完成的上限，超时后强制关闭并返回错误。
	defaultDrainTimeout = 30 * time.Second
)
