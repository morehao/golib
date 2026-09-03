package gtrace

import "sync"

var (
	globalMu     sync.RWMutex
	globalTracer Tracer = noopTracer{}
)

// T returns the process-wide Tracer. It defaults to a Noop implementation that
// still generates and propagates trace ids; call SetTracer to opt into a real
// tracing implementation (e.g. from golib/gtrace/otel).
func T() Tracer {
	globalMu.RLock()
	defer globalMu.RUnlock()
	return globalTracer
}

// SetTracer replaces the process-wide Tracer. Passing nil resets to Noop.
// Note: current implementations do not coordinate a global "enabled" flag with
// GTracer configuration; callers that want to fully disable should SetTracer(nil).
func SetTracer(t Tracer) {
	globalMu.Lock()
	defer globalMu.Unlock()
	if t == nil {
		globalTracer = noopTracer{}
		return
	}
	globalTracer = t
}
