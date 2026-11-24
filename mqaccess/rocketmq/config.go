package rocketmq

import (
	"errors"
	"time"
)

// Config 是 RocketMQ 引擎的专属配置。
//
// 跨引擎一致的配置（日志、callerSkip）走 mqaccess.Option，不放在这里。
// 这里只放 RocketMQ 自身的连接 / 凭据 / 超时参数。
type Config struct {
	// Service 服务名，必填；落日志 service 字段，也用于日志身份区分。
	Service string
	// NameServer RocketMQ NameServer 地址，逗号分隔可多个（如 "ns1:9876;ns2:9876"），必填。
	NameServer string
	// GroupName 生产者 / 消费者组名，必填。
	GroupName string

	// AccessKey / SecretKey / SecurityToken ACL 凭据（可选；未开启 ACL 可留空）。
	AccessKey     string
	SecretKey     string
	SecurityToken string

	// SendMsgTimeout 发送超时（可选；<=0 时用客户端默认值）。
	SendMsgTimeout time.Duration
	// Retry 发送重试次数（可选；0 表示用客户端默认值）。
	Retry int
}

// validate 校验必填项。
func (c *Config) validate() error {
	if c == nil {
		return errors.New("rocketmq: config is nil")
	}
	if c.Service == "" {
		return errors.New("rocketmq: Service is required")
	}
	if c.NameServer == "" {
		return errors.New("rocketmq: NameServer is required")
	}
	if c.GroupName == "" {
		return errors.New("rocketmq: GroupName is required")
	}
	return nil
}
