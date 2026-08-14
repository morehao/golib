package gcron

import (
	"context"
	"testing"

	"github.com/morehao/golib/gconstant"
	"github.com/stretchr/testify/require"
)

func TestBuildTraceContext(t *testing.T) {
	ctx, span, traceID, spanID, requestID := buildTraceContext(context.Background(), "test-task")
	defer span.End()
	require.NotEmpty(t, traceID)
	require.NotEmpty(t, spanID)
	require.NotEmpty(t, requestID)
	require.Equal(t, traceID, ctx.Value(gconstant.KeyTraceID))
	require.Equal(t, spanID, ctx.Value(gconstant.KeySpanID))
	require.Equal(t, requestID, ctx.Value(gconstant.KeyAppRequestID))
}
