package ginasync

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/morehao/golib/biz/gcontext/gincontext"
	"github.com/morehao/golib/task/gasync"
)

// failAsyncTaskOp 把任务中心错误映射到 HTTP 状态码，其余错误沿用本包统一的 JSON
// envelope（HTTP 200 + code != 0，见 gincontext.Fail）。
//
// 状态码映射：
//   - gasync.ErrAsyncTaskNotFound / gasync.ErrRunNotFound → 404（资源不存在）；
//   - gasync.ErrNotReplayable → 409（任务非终态，当前状态不允许重放）。
//
// 响应体由 gincontext.FailWithStatus 统一构造，与 ginasync.failAsyncOp 保持同一套约定。
func failAsyncTaskOp(c *gin.Context, err error) {
	switch {
	case errors.Is(err, gasync.ErrAsyncTaskNotFound), errors.Is(err, gasync.ErrRunNotFound):
		gincontext.FailWithStatus(c, http.StatusNotFound, err)
	case errors.Is(err, gasync.ErrNotReplayable):
		gincontext.FailWithStatus(c, http.StatusConflict, err)
	default:
		gincontext.Fail(c, err)
	}
}
