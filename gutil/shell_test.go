package gutil

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestExecShell(t *testing.T) {
	out, err := ExecShell("echo hello")
	require.NoError(t, err)
	require.Equal(t, "hello", out)
}

func TestExecShellError(t *testing.T) {
	out, err := ExecShell("echo boom >&2; exit 3")
	require.Error(t, err)
	// 输出（含 stderr）仍带回
	require.Contains(t, out, "boom")
}

func TestExecShellContextCancel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := ExecShellContext(ctx, "sleep 5")
	require.Error(t, err)
	require.True(t, errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled))
}

func TestExecShellTrimsTrailingNewline(t *testing.T) {
	out, err := ExecShell("printf 'a\\nb\\n\\n'")
	require.NoError(t, err)
	require.Equal(t, "a\nb", out)
	require.False(t, strings.HasSuffix(out, "\n"))
}
