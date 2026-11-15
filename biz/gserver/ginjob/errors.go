package ginjob

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/morehao/golib/biz/gcontext/gincontext"
	"github.com/morehao/golib/task/gjob"
)

// failJobOp 把任务中心错误映射到 HTTP 状态码，其余错误沿用本包统一的 JSON
// envelope（HTTP 200 + code != 0，见 gincontext.Fail）。
//
// 状态码映射：
//   - gjob.ErrJobNotFound / gjob.ErrRunNotFound → 404（资源不存在）；
//   - gjob.ErrNotReplayable → 409（任务非终态，当前状态不允许重放）。
//
// 响应体由 gincontext.FailWithStatus 统一构造，与 ginasync.failAsyncOp 保持同一套约定。
func failJobOp(c *gin.Context, err error) {
	switch {
	case errors.Is(err, gjob.ErrJobNotFound), errors.Is(err, gjob.ErrRunNotFound):
		gincontext.FailWithStatus(c, http.StatusNotFound, err)
	case errors.Is(err, gjob.ErrNotReplayable):
		gincontext.FailWithStatus(c, http.StatusConflict, err)
	default:
		gincontext.Fail(c, err)
	}
}
