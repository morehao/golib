package ginupload

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/morehao/golib/biz/gcontext/gincontext"
	"github.com/morehao/golib/filestore"
)

// failFileOp 把需要以非 200 状态码表达的业务错误映射为 HTTP 409（资源冲突），
// 其余错误沿用本包统一的 JSON envelope（HTTP 200 + code != 0，见 gincontext.Fail）。
//
// 409 的业务含义：
//   - filestore.ErrContentExists：同 content_hash 的内容已存在，无需重复上传；
//   - filestore.ErrSizeMismatch：complete 时服务端实测大小与 init 声明不一致。
//
// 响应体由 gincontext.FailWithStatus 统一构造（code/requestID/msg/data），
// 本包不自行拼 envelope —— 否则 code 默认值、msg 取值等约定会被复制一份，
// gincontext 改动时这里不会跟着变，形成静默分歧。
func failFileOp(c *gin.Context, err error) {
	if errors.Is(err, filestore.ErrContentExists) || errors.Is(err, filestore.ErrSizeMismatch) {
		gincontext.FailWithStatus(c, http.StatusConflict, err)
		return
	}
	gincontext.Fail(c, err)
}
