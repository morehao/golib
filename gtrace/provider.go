package gtrace

import (
	"context"
	"errors"
	"sync"

	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

type Provider struct {
	tp         *sdktrace.TracerProvider
	propagator propagation.TextMapPropagator

	shutdownOnce sync.Once
	shutdownErr  error
}

func (p *Provider) Shutdown(ctx context.Context) error {
	if p == nil || p.tp == nil {
		return nil
	}

	p.shutdownOnce.Do(func() {
		p.shutdownErr = p.tp.Shutdown(ctx)
	})

	return p.shutdownErr
}

func (p *Provider) TracerProvider() *sdktrace.TracerProvider {
	if p == nil {
		return nil
	}
	return p.tp
}

func (p *Provider) Propagator() propagation.TextMapPropagator {
	if p == nil {
		return nil
	}
	return p.propagator
}

func (p *Provider) ForceFlush(ctx context.Context) error {
	if p == nil || p.tp == nil {
		return nil
	}

	if err := p.tp.ForceFlush(ctx); err != nil {
		return errors.New("trace force flush failed: " + err.Error())
	}

	return nil
}
