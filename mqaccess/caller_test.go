package mqaccess

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/morehao/golib/glog"
	_ "github.com/morehao/golib/glog/driver/zap"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var logCallerRe = regexp.MustCompile(`"caller":"([^"]+)"`)

// newCallerTestLogger 创建带文件 writer 的记录层 logger，调用方负责 Close 刷盘。
func newCallerTestLogger(t *testing.T, dir string, skip int) *Logger {
	t.Helper()
	cfg := &glog.LogConfig{
		Service: "mqaccess-caller-test",
		Module:  "test",
		Level:   glog.DebugLevel,
		Writers: []glog.WriterConfig{{Type: glog.WriterFile, Dir: dir}},
	}
	l, err := NewLogger("svc", "addr", cfg, skip)
	require.NoError(t, err)
	return l
}

// readLogCallers 读取目录下所有日志文件的 caller 字段。
func readLogCallers(t *testing.T, dir string) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "*.log"))
	require.NoError(t, err)
	require.NotEmpty(t, files, "no log files under %s", dir)

	var out []string
	for _, f := range files {
		b, err := os.ReadFile(f)
		require.NoError(t, err)
		for _, m := range logCallerRe.FindAllSubmatch(b, -1) {
			out = append(out, string(m[1]))
		}
	}
	return out
}

// splitCaller 把 file:line 拆成 (base, line)。
func splitCaller(t *testing.T, caller string) (string, int) {
	t.Helper()
	idx := strings.LastIndex(caller, ":")
	require.Greater(t, idx, 0, "bad caller %q", caller)
	line, err := strconv.Atoi(caller[idx+1:])
	require.NoError(t, err)
	return filepath.Base(caller[:idx]), line
}

// logFromEngineHelper 模拟引擎里「Record 的直接调用方」这一帧，返回 Record 所在行号。
func logFromEngineHelper(l *Logger, ctx context.Context) int {
	_, _, line, _ := runtime.Caller(0)
	l.Record(ctx, "publish", time.Now(), nil) // line + 1
	return line + 1
}

// TestRecordCallerSkipOneFrame 锁定「skip=1 指向直接调用 Record 的那一行」这一约定：
// 各引擎的默认 callerSkip 都是基于该约定推导出来的。
func TestRecordCallerSkipOneFrame(t *testing.T) {
	dir := t.TempDir()
	l := newCallerTestLogger(t, dir, 1)
	wantLine := logFromEngineHelper(l, context.Background())
	require.NoError(t, l.Underlying().Close())

	callers := readLogCallers(t, dir)
	require.Len(t, callers, 1)
	file, line := splitCaller(t, callers[0])
	assert.Equal(t, "caller_test.go", file)
	assert.Equal(t, wantLine, line)
}

// TestRecordCallerSkipTwoFrames 锁定「skip=2 指向调用引擎方法的那一行（业务行）」：
// NATS 的默认值 2 正是依赖这个约定。
func TestRecordCallerSkipTwoFrames(t *testing.T) {
	dir := t.TempDir()
	l := newCallerTestLogger(t, dir, 2)

	_, _, anchorLine, _ := runtime.Caller(0)
	logFromEngineHelper(l, context.Background()) // anchorLine + 1
	require.NoError(t, l.Underlying().Close())

	callers := readLogCallers(t, dir)
	require.Len(t, callers, 1)
	file, line := splitCaller(t, callers[0])
	assert.Equal(t, "caller_test.go", file)
	assert.Equal(t, anchorLine+1, line)
}

// TestResolveCallerSkip 验证 skip 解析优先级：显式值 > 引擎默认 > 包级兜底。
func TestResolveCallerSkip(t *testing.T) {
	assert.Equal(t, 5, ResolveCallerSkip(5, 2))
	assert.Equal(t, 2, ResolveCallerSkip(0, 2))
	assert.Equal(t, 4, ResolveCallerSkip(-1, 0))
	assert.Equal(t, DefaultCallerSkip, ResolveCallerSkip(0, 0))
}
