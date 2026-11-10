package ginasync

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/morehao/golib/biz/gcontext/gincontext"
	"github.com/morehao/golib/task/gasync"
)

// failAsyncOp 把「资源不存在」映射为 HTTP 404，其余错误沿用本包统一的
// JSON envelope（HTTP 200 + code != 0，见 gincontext.Fail）。
//
// 404 的业务含义：
//   - gasync.ErrTaskNotFound：任务类型未注册（详情/启停不存在的类型）；
//   - gasync.ErrRunNotFound：运行记录不存在（详情查询传入未知 ID）。
//
// 响应体由 gincontext.FailWithStatus 统一构造，与 ginupload.failFileOp 保持同一套约定。
func failAsyncOp(c *gin.Context, err error) {
	if errors.Is(err, gasync.ErrTaskNotFound) || errors.Is(err, gasync.ErrRunNotFound) {
		gincontext.FailWithStatus(c, http.StatusNotFound, err)
		return
	}
	gincontext.Fail(c, err)
}
