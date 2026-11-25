package nats

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

	"github.com/morehao/golib/gconstant"
	"github.com/morehao/golib/glog"
	"github.com/morehao/golib/mqaccess"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nkeys"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var calibCallerRe = regexp.MustCompile(`"caller":"([^"]+)"`)

// newCalibLogConfig 返回写入临时目录的日志配置。
func newCalibLogConfig(dir string) *glog.LogConfig {
	return &glog.LogConfig{
		Service: "mqaccess-nats-calib",
		Module:  "test",
		Level:   glog.DebugLevel,
		Writers: []glog.WriterConfig{{Type: glog.WriterFile, Dir: dir}},
	}
}

// readCalibLog 读取目录下所有日志内容。
func readCalibLog(t *testing.T, dir string) string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "*.log"))
	require.NoError(t, err)
	var sb strings.Builder
	for _, f := range files {
		b, err := os.ReadFile(f)
		require.NoError(t, err)
		sb.Write(b)
	}
	return sb.String()
}

// callerLines 从日志内容里取出 caller 行号集合。
func callerLines(t *testing.T, content string) []int {
	t.Helper()
	var out []int
	for _, m := range calibCallerRe.FindAllStringSubmatch(content, -1) {
		idx := strings.LastIndex(m[1], ":")
		require.Greater(t, idx, 0)
		line, err := strconv.Atoi(m[1][idx+1:])
		require.NoError(t, err)
		out = append(out, line)
	}
	return out
}

// TestNewNKeySeedFileInvalid 验证 NKeySeedFile 按「文件路径」语义读取：
// 传入不存在的文件时，New 在建连前就报错（回归旧实现把字段当「种子串」的误导）。
func TestNewNKeySeedFileInvalid(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "not-exist.nk")
	c, err := New(&Config{Service: "nkey", URL: "nats://127.0.0.1:1", NKeySeedFile: missing})
	require.Error(t, err)
	assert.Nil(t, c)
	assert.Contains(t, err.Error(), "nkey")
	assert.Contains(t, err.Error(), missing)
}

// TestNewNKeySeedFileValidThenConnectFails 验证合法的种子文件能通过 option 构建，
// 失败点落在「建连」而不是「加载种子」。
func TestNewNKeySeedFileValidThenConnectFails(t *testing.T) {
	dir := t.TempDir()
	seedPath := filepath.Join(dir, "user.nk")

	kp, err := nkeys.CreateUser()
	require.NoError(t, err)
	seed, err := kp.Seed()
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(seedPath, seed, 0o600))

	c, err := New(&Config{
		Service:        "nkey",
		URL:            "nats://127.0.0.1:1", // 无服务端，快速失败
		NKeySeedFile:   seedPath,
		ConnectTimeout: 300 * time.Millisecond,
	})
	require.Error(t, err)
	assert.Nil(t, c)
	assert.Contains(t, err.Error(), "connect")
	assert.NotContains(t, err.Error(), "seed file")
}

// TestPublishCallerCalibrationShape 用与 Client.Publish 相同的帧形状
// （Record ← 引擎方法 ← 业务）验证默认 callerSkip=2 能落到业务行。
// 无 broker 也能跑；有 broker 时另有 TestPublishCallerCalibrated 做端到端校验。
func TestPublishCallerCalibrationShape(t *testing.T) {
	dir := t.TempDir()
	// 直接取引擎默认值，确保被测的是 defaultCallerSkip 而非测试自造的数字。
	l, err := mqaccess.NewLogger("svc", "addr", newCalibLogConfig(dir),
		mqaccess.ResolveCallerSkip(0, defaultCallerSkip))
	require.NoError(t, err)

	_, _, anchorLine, _ := runtime.Caller(0)
	publishLikeBusiness(t, l) // anchorLine + 1
	require.NoError(t, l.Underlying().Close())

	lines := callerLines(t, readCalibLog(t, dir))
	require.Len(t, lines, 1)
	assert.Equal(t, anchorLine+1, lines[0], "NATS 默认 callerSkip 应定位到业务调用行")
}

// publishLikeBusiness 模拟 Client.Publish 这一帧。
func publishLikeBusiness(t *testing.T, l *mqaccess.Logger) {
	t.Helper()
	l.Record(context.Background(), "publish", time.Now(), nil,
		gconstant.KeyDbOperationContent, "calib.subject")
}

// TestCloseIsIdempotent 验证重复 Close 不报错（回归：Drain 在已关闭连接上返回错）。
func TestCloseIsIdempotent(t *testing.T) {
	url := requireNATS(t)
	client, err := New(&Config{Service: "idempotent", URL: url})
	require.NoError(t, err)

	require.NoError(t, client.Close())
	assert.NoError(t, client.Close(), "二次 Close 应幂等返回 nil")
}

// TestCloseWaitsForDrain 验证 Close 会等 Drain 完成：仍在执行的回调必须先结束，
// Close 才返回（回归：Drain 异步启动后立刻 Close 会打断排空）。
func TestCloseWaitsForDrain(t *testing.T) {
	url := requireNATS(t)
	client, err := New(&Config{Service: "drain", URL: url})
	require.NoError(t, err)

	subject := "mqaccess.test.drain." + randSuffix(t)
	done := make(chan struct{})
	sub, err := client.Subscribe(subject, func(ctx context.Context, msg *nats.Msg) {
		time.Sleep(50 * time.Millisecond)
		close(done)
	})
	require.NoError(t, err)
	defer func() { _ = sub.Unsubscribe() }()

	require.NoError(t, client.Conn().Flush())
	require.NoError(t, client.Publish(context.Background(), subject, []byte("drain"), nil))
	require.NoError(t, client.Conn().Flush())

	require.NoError(t, client.Close())
	select {
	case <-done:
	default:
		t.Fatal("Close 在订阅回调结束前返回，Drain 没有生效")
	}
}

// TestPublishCallerCalibrated 端到端校验默认 callerSkip：需要真实 NATS。
func TestPublishCallerCalibrated(t *testing.T) {
	url := requireNATS(t)
	dir := t.TempDir()
	client, err := New(&Config{Service: "calib", URL: url}, mqaccess.WithLogConfig(newCalibLogConfig(dir)))
	require.NoError(t, err)

	anchorLine := publishBusinessLine(t, client, "mqaccess.test.calib."+randSuffix(t))
	require.NoError(t, client.Close())
	require.NoError(t, client.logger.Underlying().Close())

	lines := callerLines(t, readCalibLog(t, dir))
	require.NotEmpty(t, lines)
	assert.Contains(t, lines, anchorLine, "caller 应指向业务调用 Publish 的那一行")
}

// publishBusinessLine 返回调用 Publish 的那一行行号。
func publishBusinessLine(t *testing.T, c *Client, subject string) int {
	t.Helper()
	_, _, line, _ := runtime.Caller(0)
	require.NoError(t, c.Publish(context.Background(), subject, []byte("x"), nil))
	return line + 1
}
