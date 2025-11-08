package protocol

import "time"

type HttpClientConfig struct {
	Module   string        `yaml:"module"`
	Host     string        `yaml:"host"`
	Timeout  time.Duration `yaml:"timeout"`
	MaxRetry int           `yaml:"max_retry"`
}

type SSEClientConfig struct {
	Module        string        `yaml:"service"`
	Host          string        `yaml:"host"`
	RetryWaitTime time.Duration `yaml:"retry_timeout"`
	MaxRetry      int           `yaml:"max_retry"`
}
