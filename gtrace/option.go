package gtrace

import (
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
)

type Option interface {
	apply(*optConfig)
}

type optionFunc func(*optConfig)

func (fn optionFunc) apply(cfg *optConfig) {
	fn(cfg)
}

type optConfig struct {
	propagator propagation.TextMapPropagator
	resource   *resource.Resource
	idGen      trace.IDGenerator
}

func WithPropagator(p propagation.TextMapPropagator) Option {
	return optionFunc(func(cfg *optConfig) {
		cfg.propagator = p
	})
}

func WithResource(r *resource.Resource) Option {
	return optionFunc(func(cfg *optConfig) {
		cfg.resource = r
	})
}

func WithIDGenerator(idGen trace.IDGenerator) Option {
	return optionFunc(func(cfg *optConfig) {
		cfg.idGen = idGen
	})
}
