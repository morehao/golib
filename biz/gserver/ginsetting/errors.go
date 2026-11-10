package ginsetting

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/morehao/golib/biz/gcontext/gincontext"
	"github.com/morehao/golib/gsetting"
)

// failSettingOp 把「配置项不存在」映射为 HTTP 404，其余错误沿用本包统一的
// JSON envelope（HTTP 200 + code != 0，见 gincontext.Fail）。
//
// 响应体由 gincontext.FailWithStatus 统一构造，与 ginupload.failFileOp 保持同一套约定。
func failSettingOp(c *gin.Context, err error) {
	if errors.Is(err, gsetting.ErrSettingNotFound) {
		gincontext.FailWithStatus(c, http.StatusNotFound, err)
		return
	}
	gincontext.Fail(c, err)
}
