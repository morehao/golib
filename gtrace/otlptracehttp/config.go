package otlptracehttp

import "time"

const (
	CompressionNone = "none"
	CompressionGzip = "gzip"

	DefaultURLPath = "/v1/traces"
)

type Config struct {
	Endpoint    string
	URLPath     string
	Insecure    bool
	Headers     map[string]string
	Timeout     time.Duration
	Compression string
}

func DefaultConfig() Config {
	return Config{
		Insecure: false,
		Timeout:  10 * time.Second,
		URLPath:  DefaultURLPath,
	}
}
