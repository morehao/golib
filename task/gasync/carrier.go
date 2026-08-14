package gasync

import (
	"go.opentelemetry.io/otel/propagation"
)

type headerCarrier map[string]string

func (c headerCarrier) Get(key string) string { return c[key] }
func (c headerCarrier) Set(key string, value string) {
	c[key] = value
}
func (c headerCarrier) Keys() []string {
	out := make([]string, 0, len(c))
	for k := range c {
		out = append(out, k)
	}
	return out
}

var _ propagation.TextMapCarrier = headerCarrier{}
