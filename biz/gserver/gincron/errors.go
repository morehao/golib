package gincron

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/morehao/golib/biz/gcontext/gincontext"
	"github.com/morehao/golib/task/gcron"
)

// failCronOp 把「资源不存在」映射为 HTTP 404，其余错误沿用本包统一的
// JSON envelope（HTTP 200 + code != 0，见 gincontext.Fail）。
//
// 404 的业务含义：
//   - gcron.ErrTaskNotFound：任务未注册（详情/启停/删除不存在的任务）；
//   - gcron.ErrRunNotFound：运行记录不存在（详情查询传入未知 ID）。
//
// 响应体同样由 gincontext.FailWithStatus 统一构造，不在此处自行拼 envelope，
// 与 ginupload.failFileOp 保持同一套约定。
func failCronOp(c *gin.Context, err error) {
	if errors.Is(err, gcron.ErrTaskNotFound) || errors.Is(err, gcron.ErrRunNotFound) {
		gincontext.FailWithStatus(c, http.StatusNotFound, err)
		return
	}
	gincontext.Fail(c, err)
}
