package glog_test

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
	_ "github.com/morehao/golib/glog/driver/slog"
	_ "github.com/morehao/golib/glog/driver/zap"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var callerRe = regexp.MustCompile(`"caller":"([^"]+)"`)

func logViaWrapper(l glog.Logger, ctx context.Context, msg string) {
	l.Infow(ctx, msg)
}

func readCallerField(t *testing.T, logPath string) string {
	t.Helper()
	b, err := os.ReadFile(logPath)
	require.NoError(t, err, "read log %s", logPath)
	m := callerRe.FindSubmatch(b)
	require.NotNil(t, m, "caller not found in %s: %s", logPath, string(b))
	return string(m[1])
}

func normalizeCaller(c string) (file string, line int) {
	idx := strings.LastIndex(c, ":")
	line, _ = strconv.Atoi(c[idx+1:])
	return filepath.Base(c[:idx]), line
}

func newCallerTestLogger(t *testing.T, dir string, lt glog.LoggerType, skip int) (glog.Logger, string) {
	t.Helper()
	fileName := "slog.log"
	logPath := filepath.Join(dir, time.Now().Format("20060102"), "slog_full.log")
	if lt == glog.LoggerTypeZap {
		fileName = "zap.log"
		logPath = filepath.Join(dir, time.Now().Format("20060102"), "zap.log")
	}
	cfg := &glog.LogConfig{
		Service:    "caller-test",
		Module:     "test",
		Level:      glog.InfoLevel,
		Writers:    []glog.WriterConfig{{Type: glog.WriterFile, Dir: dir, FileName: fileName}},
		LoggerType: lt,
	}
	l, err := glog.NewLogger(cfg, glog.WithCallerSkip(skip))
	require.NoError(t, err)
	return l, logPath
}

func TestCallerSkipConsistentAcrossDrivers(t *testing.T) {
	results := map[glog.LoggerType]struct{ skip0, skip1 string }{}

	for _, lt := range []glog.LoggerType{glog.LoggerTypeZap, glog.LoggerTypeSlog} {
		dir := t.TempDir()
		l0, path0 := newCallerTestLogger(t, dir, lt, 0)
		logViaWrapper(l0, context.Background(), "msg")
		require.NoError(t, l0.Close())
		skip0 := readCallerField(t, path0)

		dir1 := t.TempDir()
		l1, path1 := newCallerTestLogger(t, dir1, lt, 1)
		_, _, anchorLine, _ := runtime.Caller(0)
		logViaWrapper(l1, context.Background(), "msg")
		require.NoError(t, l1.Close())
		skip1 := readCallerField(t, path1)

		results[lt] = struct{ skip0, skip1 string }{skip0: skip0, skip1: skip1}
		t.Logf("%s skip0=%s skip1=%s expectLine=%d", lt, skip0, skip1, anchorLine+1)

		_, line := normalizeCaller(skip1)
		assert.Equal(t, anchorLine+1, line, "%s: skip=1 应定位到调用 logViaWrapper 的那一行", lt)
	}

	zapSkip0File, zapSkip0Line := normalizeCaller(results[glog.LoggerTypeZap].skip0)
	slogSkip0File, slogSkip0Line := normalizeCaller(results[glog.LoggerTypeSlog].skip0)
	assert.Equal(t, zapSkip0File, slogSkip0File, "skip=0 时两个 driver 的 caller 文件应一致")
	assert.Equal(t, zapSkip0Line, slogSkip0Line, "skip=0 时两个 driver 的 caller 行应一致")

	zapSkip1File, zapSkip1Line := normalizeCaller(results[glog.LoggerTypeZap].skip1)
	slogSkip1File, slogSkip1Line := normalizeCaller(results[glog.LoggerTypeSlog].skip1)
	assert.Equal(t, zapSkip1File, slogSkip1File, "skip=1 时两个 driver 的 caller 文件应一致")
	assert.Equal(t, zapSkip1Line, slogSkip1Line, "skip=1 时两个 driver 的 caller 行应一致")
}

func TestCallerPackageLevelCall(t *testing.T) {
	dir := t.TempDir()
	for _, lt := range []glog.LoggerType{glog.LoggerTypeZap, glog.LoggerTypeSlog} {
		fileName := "slog.log"
		logPath := filepath.Join(dir, time.Now().Format("20060102"), "slog_full.log")
		if lt == glog.LoggerTypeZap {
			fileName = "zap.log"
			logPath = filepath.Join(dir, time.Now().Format("20060102"), "zap.log")
		}
		cfg := &glog.LogConfig{
			Service:    "caller-test",
			Module:     "test",
			Level:      glog.InfoLevel,
			Writers:    []glog.WriterConfig{{Type: glog.WriterFile, Dir: dir, FileName: fileName}},
			LoggerType: lt,
		}
		require.NoError(t, glog.InitLogger(cfg))
		_, _, anchorLine, _ := runtime.Caller(0)
		glog.Infow(context.Background(), "msg")
		require.NoError(t, glog.Close())

		caller := readCallerField(t, logPath)
		_, line := normalizeCaller(caller)
		assert.Equal(t, anchorLine+1, line, "%s: 包级调用 skip=0 应定位到调用 glog.Infow 的那一行", lt)
	}
}
