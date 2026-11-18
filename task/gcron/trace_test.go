package gcron

import (
	"context"
	"testing"

	"github.com/morehao/golib/gconstant"
	"github.com/stretchr/testify/require"
)

func TestBuildTraceContext(t *testing.T) {
	ctx, span, requestID := buildTraceContext(context.Background(), "test-task")
	defer span.End()
	require.NotEmpty(t, requestID)
	// traceID 不再单独返回：它由 InjectTraceFields 写进 ctx，这里直接断言 ctx 里有值。
	require.NotEmpty(t, ctx.Value(gconstant.KeyTraceID))
	require.Equal(t, requestID, ctx.Value(gconstant.KeyAppRequestID))
}
