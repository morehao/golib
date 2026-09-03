package gtrace

// TextMapCarrier is the transport container used to propagate / extract span
// context across process boundaries (HTTP headers, asynq task headers, ...).
// It mirrors a minimal subset of otel's propagation.TextMapCarrier.
type TextMapCarrier interface {
	// Get returns the value for the key, empty string when absent.
	Get(key string) string
	// Set stores the value for the key.
	Set(key string, value string)
	// Keys returns the keys present in the carrier.
	Keys() []string
}

// traceparentHeader / tracestateHeader are the W3C header names used by Inject.
const (
	TraceparentHeader = "traceparent"
	TracestateHeader  = "tracestate"
)
