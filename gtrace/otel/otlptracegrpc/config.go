package otlptracegrpc

import "time"

type Config struct {
	Endpoint    string
	Insecure    bool
	Headers     map[string]string
	Timeout     time.Duration
	Compression string
}

func DefaultConfig() Config {
	return Config{
		Insecure: false,
		Timeout:  10 * time.Second,
	}
}
