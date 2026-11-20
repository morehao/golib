package ginupload

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/morehao/golib/biz/gserver/ginserver"
	"github.com/morehao/golib/dbaccess/gormplugin"
	"github.com/morehao/golib/filestore"
	_ "github.com/morehao/golib/storage/driver/local"
	"github.com/stretchr/testify/require"
)

// testAppContextFor 返回携带指定业务线（app）的 ctx，供直接调用组件 API 用。
func testAppContextFor(app string) context.Context {
	return gormplugin.WithApp(context.Background(), app)
}

// appInjectMiddleware 把请求绑定的业务线（app）注入 request ctx，模拟接入方在
// 预签名消费端点上应有的 app 注入中间件（Register 本身不内置中间件，由调用方 group.Use）。
func appInjectMiddleware(app string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Request = c.Request.WithContext(gormplugin.WithApp(c.Request.Context(), app))
		c.Next()
	}
}

// newObjectsRouter 注册完整路由组并挂 app 注入中间件。
func newObjectsRouter(fs *filestore.FileStore, app string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	group := ginserver.NewRouterGroups(r, testAppName, []ginserver.VersionGroup{{Version: testVersion}}).MustGetGroup(testVersion)
	group.Use(appInjectMiddleware(app))
	Register(group, fs)
	return r
}

// TestPresignedGet_EnforcesAppBinding 锁定 C3：预签名消费端点必须校验 token.app 与
// 请求 ctx 的 app 一致——此前 handler 用 ParsePresignedToken，剥掉 app 段却不校验，
// 使"防跨 app 转发 URL 越权"只在单测里成立、生产不生效。
//
// 覆盖：app-a 签发下载 URL → app-a 消费成功、app-b 消费 403。
func TestPresignedGet_EnforcesAppBinding(t *testing.T) {
	fs := newLocalFileStore(t, t.TempDir(), filestore.WithAppScope())

	// app-a 上传一个真实文件（经组件 API，app 由 ctx 携带）。
	ctxA := testAppContextFor("app-a")
	detail, err := fs.UploadAndRecord(ctxA, filestore.UploadAndRecordRequest{
		ContentHash: sha256Hex("hello"),
		Name:        "report.txt",
		Size:        int64(len("hello")),
		Reader:      strings.NewReader("hello"),
	})
	require.NoError(t, err)

	// app-a 签发下载预签名 URL（local 驱动，URL 携带 token/expires，且已 app 绑定）。
	presigned, err := fs.PresignGetFileURL(ctxA, detail.FileUploadID)
	require.NoError(t, err)
	require.NotEmpty(t, presigned.URL)

	u, err := url.Parse(presigned.URL)
	require.NoError(t, err)
	// newLocalFileStore 的 storage BaseURL 已含 "/v1/ginupload/objects" 前缀，故 u.Path
	// 即本服务的完整路由路径（/v1/ginupload/objects/<bucket>/<key>），直接拼查询串。
	reqPath := u.Path + "?" + u.RawQuery

	// app-b 消费 app-a 的 URL：必须 403（跨 app 越权拒绝）。
	wrongRouter := newObjectsRouter(fs, "app-b")
	wWrong := httptest.NewRecorder()
	reqWrong, _ := http.NewRequest(http.MethodGet, reqPath, nil)
	wrongRouter.ServeHTTP(wWrong, reqWrong)
	require.Equal(t, http.StatusForbidden, wWrong.Code,
		"app-b 消费 app-a 的预签名下载 URL 必须被拒绝（跨 app 越权）")

	// app-a 消费自己的 URL：应成功读到内容。
	rightRouter := newObjectsRouter(fs, "app-a")
	wRight := httptest.NewRecorder()
	reqRight, _ := http.NewRequest(http.MethodGet, reqPath, nil)
	rightRouter.ServeHTTP(wRight, reqRight)
	require.Equal(t, http.StatusOK, wRight.Code,
		"app-a 消费自己的预签名下载 URL 应成功")
	require.Equal(t, "hello", wRight.Body.String())
}
