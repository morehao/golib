package gtrace

import (
	"fmt"
	"strings"
	"time"
)

type SamplerType string

const (
	SamplerAlwaysOn     SamplerType = "always_on"
	SamplerAlwaysOff    SamplerType = "always_off"
	SamplerTraceIDRatio SamplerType = "traceidratio"
)

type Config struct {
	ServiceName    string
	ServiceVersion string
	Environment    string

	Sampler      SamplerType
	TraceIDRatio float64

	MaxQueueSize       int
	MaxExportBatchSize int
	BatchTimeout       time.Duration
	ExportTimeout      time.Duration
}

type TraceConfig struct {
	Enable         bool       `yaml:"enable"`
	ServiceVersion string     `yaml:"service_version"`
	Sampler        string     `yaml:"sampler"`
	TraceIDRatio   float64    `yaml:"trace_id_ratio"`
	OTLP           OTLPConfig `yaml:"otlp"`
}

type OTLPConfig struct {
	Endpoint string        `yaml:"endpoint"`
	Insecure bool          `yaml:"insecure"`
	Timeout  time.Duration `yaml:"timeout"`
}

func DefaultConfig(serviceName string) Config {
	return Config{
		ServiceName:        serviceName,
		Sampler:            SamplerTraceIDRatio,
		TraceIDRatio:       1.0,
		MaxQueueSize:       2048,
		MaxExportBatchSize: 512,
		BatchTimeout:       5 * time.Second,
		ExportTimeout:      30 * time.Second,
	}
}

func ValidateConfig(cfg Config) error {
	if strings.TrimSpace(cfg.ServiceName) == "" {
		return fmt.Errorf("service name is empty")
	}

	switch cfg.Sampler {
	case "", SamplerAlwaysOn, SamplerAlwaysOff, SamplerTraceIDRatio:
	default:
		return fmt.Errorf("unsupported sampler type: %s", cfg.Sampler)
	}

	if cfg.TraceIDRatio < 0 || cfg.TraceIDRatio > 1 {
		return fmt.Errorf("trace id ratio out of range [0,1]: %f", cfg.TraceIDRatio)
	}

	if cfg.MaxQueueSize <= 0 {
		return fmt.Errorf("max queue size must be greater than 0")
	}
	if cfg.MaxExportBatchSize <= 0 {
		return fmt.Errorf("max export batch size must be greater than 0")
	}
	if cfg.MaxExportBatchSize > cfg.MaxQueueSize {
		return fmt.Errorf("max export batch size must be less than or equal to max queue size")
	}
	if cfg.BatchTimeout <= 0 {
		return fmt.Errorf("batch timeout must be greater than 0")
	}
	if cfg.ExportTimeout <= 0 {
		return fmt.Errorf("export timeout must be greater than 0")
	}

	return nil
}

func ParseSampler(sampler string) (SamplerType, error) {
	switch strings.ToLower(strings.TrimSpace(sampler)) {
	case "", string(SamplerTraceIDRatio):
		return SamplerTraceIDRatio, nil
	case string(SamplerAlwaysOn):
		return SamplerAlwaysOn, nil
	case string(SamplerAlwaysOff):
		return SamplerAlwaysOff, nil
	default:
		return "", fmt.Errorf("unsupported trace sampler: %s", sampler)
	}
}
