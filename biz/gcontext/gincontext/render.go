package gincontext

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/morehao/golib/biz/gcontext"
	"github.com/morehao/golib/gerror"
)

type DtoRender struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data any    `json:"data"`
}

func Success(ctx *gin.Context, data any) {
	renderSuccess(ctx, data, false)
}

func SuccessWithFormat(ctx *gin.Context, data any) {
	renderSuccess(ctx, data, true)
}

func renderSuccess(ctx *gin.Context, data any, withFormat bool) {
	r := gcontext.NewResponseRender()
	r.SetCode(0)
	r.SetMsg("success")
	if withFormat {
		r.SetDataWithFormat(data)
	} else {
		r.SetData(data)
	}
	ctx.JSON(http.StatusOK, r)
}

func Fail(ctx *gin.Context, err error) {
	r := buildErrorResponse(err)
	ctx.JSON(http.StatusOK, r)
}

func Abort(ctx *gin.Context, err error) {
	r := buildErrorResponse(err)
	ctx.AbortWithStatusJSON(http.StatusOK, r)
}

func buildErrorResponse(err error) gcontext.ResponseRender {
	r := gcontext.NewResponseRender()
	var gErr gerror.Error
	if errors.As(err, &gErr) {
		r.SetCode(gErr.Code)
		r.SetMsg(gErr.Msg)
	} else {
		r.SetCode(-1)
		r.SetMsg(cause(err).Error())
	}
	r.SetData(gin.H{})
	return r
}

func cause(err error) error {
	for {
		unwrapped := errors.Unwrap(err)
		if unwrapped == nil {
			return err
		}
		err = unwrapped
	}
}
