package ghttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/morehao/golib/gconstant"
	"github.com/morehao/golib/glog"
	_ "github.com/morehao/golib/glog/driver/zap"
	"github.com/morehao/golib/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLogFieldsAreVisibleToFieldHook 断言 ghttp 的结构化日志字段用标准 k/v 形式写出，
// 因此 glog 的字段钩子能看见并改写 url.full。
//
// 为什么这条性质必须被钉住：外部只能把脱敏挂在 glog 的**字段钩子**上，而钩子按字段名命中。
// 如果日志参数被写成 glog.KV(...)（结构体参数），两个驱动都会退化成 !badKeyN 这种键，
// 钩子看不到 url.full，凭据就明文进日志了 —— 而且不会有任何用例变红。
func TestLogFieldsAreVisibleToFieldHook(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "log")
	cfg := &glog.LogConfig{
		Service:    "ghttplog",
		Module:     "test",
		Level:      glog.InfoLevel,
		Writers:    []glog.WriterConfig{{Type: glog.WriterFile, Dir: dir}},
		LoggerType: glog.LoggerTypeZap,
	}

	var seenKeys []string
	hook := func(fields []glog.Field) {
		for i := range fields {
			seenKeys = append(seenKeys, fields[i].Key)
			if fields[i].Key == gconstant.KeyUrlFull {
				if _, ok := fields[i].Value.(string); ok {
					fields[i].Value = "MASKED"
				}
			}
		}
	}
	// 本用例依赖全局 logger 覆盖 ghttp 使用的 glog 包级入口；同包测试默认串行执行，
	// 若将来给本包其他用例加 t.Parallel，需要重新设计这里的隔离方式。
	require.NoError(t, glog.InitLogger(cfg, glog.WithFieldHookFunc(hook)))
	t.Cleanup(func() {
		_ = glog.Close()
		_ = glog.InitLogger(glog.GetDefaultLogConfig())
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)

	c := NewClient(&protocol.HttpClientConfig{Module: "amap", Host: srv.URL, Timeout: 3 * time.Second})
	_, err := c.Get(context.Background(), "/v3/place/text", RequestOption{
		RequestBody: map[string]string{"key": "super-secret"},
	})
	require.NoError(t, err)

	require.NoError(t, glog.Close())
	assert.Contains(t, seenKeys, gconstant.KeyUrlFull, "字段钩子必须能看见 url.full（脱敏的唯一抓手）")
	assert.NotContains(t, strings.Join(seenKeys, ","), "!badKey", "字段名不得退化成 !badKeyN（说明参数不是 k/v 形式）")

	// 日期只取一次：读取时再取一次的话，跨零点会去错目录而偶发失败
	day := time.Now().Format("20060102")
	raw, readErr := os.ReadFile(filepath.Join(dir, day, "ghttplog_full.log"))
	require.NoError(t, readErr)
	content := string(raw)
	assert.Contains(t, content, "MASKED", "钩子的改写必须落到日志文件里")
	assert.NotContains(t, content, "super-secret", "钩子改写生效后，原值不得残留")
}
