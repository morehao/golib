package protocol

import "time"

type HttpClientConfig struct {
	Module          string        `yaml:"module"`
	Host            string        `yaml:"host"`
	Timeout         time.Duration `yaml:"timeout"`
	MaxRetry        int           `yaml:"max_retry"` // 总尝试次数（含首次），<=0 视为 1 次
	MaxIdleConns    int           `yaml:"max_idle_conns"`
	MaxConnsPerHost int           `yaml:"max_conns_per_host"`
	RetryInterval   time.Duration `yaml:"retry_interval"`    // 基础重试间隔，默认 100ms，指数退避 base*2^(n-1)，封顶 1s
	RetryOnStatus   []int         `yaml:"retry_on_status"`   // 额外重试的 HTTP 状态码（如 429/5xx），默认空则不按状态码重试
	Retryable       *bool         `yaml:"retryable"`         // 网络错误是否重试，默认 true，仅显式 false 时关闭
	IdleConnTimeout time.Duration `yaml:"idle_conn_timeout"` // 空闲连接回收时间，默认 90s
}

type SSEClientConfig struct {
	Module        string        `yaml:"service"`
	Host          string        `yaml:"host"`
	RetryWaitTime time.Duration `yaml:"retry_timeout"`
	MaxRetry      int           `yaml:"max_retry"`
}
