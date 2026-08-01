package dbredis

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/morehao/golib/glog"
	"github.com/morehao/golib/internal/testutil"
	_ "github.com/morehao/golib/glog/driver/slog"
	_ "github.com/morehao/golib/glog/driver/zap"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	testutil.Load()
}

func bizRedisCall(db *redis.Client, ctx context.Context) int {
	_, _, anchorLine, _ := runtime.Caller(0)
	db.Get(ctx, "calib_key") // anchorLine + 1
	return anchorLine + 1
}

func redisCallers(t *testing.T, dir string) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, time.Now().Format("20060102"), "*.log"))
	require.NoError(t, err)
	require.NotEmpty(t, files)
	re := regexp.MustCompile(`"caller":"([^"]+)"`)
	var out []string
	for _, f := range files {
		b, err := os.ReadFile(f)
		require.NoError(t, err)
		for _, m := range re.FindAllSubmatch(b, -1) {
			out = append(out, string(m[1]))
		}
	}
	return out
}

func TestRedisCallerSkipDefaultConsistent(t *testing.T) {
	addr, password := redisEnvConfig()
	ctx := context.Background()

	for _, lt := range []glog.LoggerType{glog.LoggerTypeZap, glog.LoggerTypeSlog} {
		dir := t.TempDir()
		cfg := &glog.LogConfig{
			Service:    "calib",
			Module:     "test",
			Level:      glog.DebugLevel,
			Writers:    []glog.WriterConfig{{Type: glog.WriterFile, Dir: dir}},
			LoggerType: lt,
		}
		client, err := New(&RedisConfig{
			Service:  "calib",
			Addr:     addr,
			Password: password,
			DB:       0,
		}, WithLogConfig(cfg))
		require.NoError(t, err)

		bizLine := bizRedisCall(client, ctx)
		require.NoError(t, client.Close())
		if lt == glog.LoggerTypeZap {
			time.Sleep(6 * time.Second)
		}

		callers := redisCallers(t, dir)
		found := false
		for _, c := range callers {
			idx := lastColonIndex(c)
			if idx < 0 {
				continue
			}
			line, _ := strconv.Atoi(c[idx+1:])
			if line == bizLine {
				found = true
				break
			}
		}
		assert.True(t, found, "%s: 默认 callerSkip 应定位到业务调用行 %d，实际 caller 列表: %v", lt, bizLine, callers)
	}
}

func lastColonIndex(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == ':' {
			return i
		}
	}
	return -1
}
