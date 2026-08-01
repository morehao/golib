package dbes

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/elastic/go-elasticsearch/v8"
	"github.com/elastic/go-elasticsearch/v8/typedapi/types"
	"github.com/morehao/golib/glog"
	_ "github.com/morehao/golib/glog/driver/slog"
	_ "github.com/morehao/golib/glog/driver/zap"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func bizEsCall(client *elasticsearch.TypedClient, ctx context.Context) int {
	_, _, anchorLine, _ := runtime.Caller(0)
	_, _ = client.Search().
		Index("test_index").
		Query(&types.Query{MatchAll: types.NewMatchAllQuery()}).
		Do(ctx) // anchorLine + 4
	return anchorLine + 4
}

func esCallers(t *testing.T, dir string) []string {
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

func mockESServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"took":1,"timed_out":false,"hits":{"total":{"value":0,"relation":"eq"},"hits":[]}}`))
	}))
}

func TestEsCallerSkipDefaultConsistent(t *testing.T) {
	srv := mockESServer(t)
	defer srv.Close()
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
		_, typed, err := New(&ESConfig{
			Service: "calib",
			Addr:    srv.URL,
		}, WithLogConfig(cfg))
		require.NoError(t, err)

		bizLine := bizEsCall(typed, ctx)
		if lt == glog.LoggerTypeZap {
			time.Sleep(6 * time.Second)
		}

		callers := esCallers(t, dir)
		found := false
		for _, c := range callers {
			idx := stringsLastIndex(c, ":")
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

func stringsLastIndex(s string, sep string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if string(s[i]) == sep {
			return i
		}
	}
	return -1
}
