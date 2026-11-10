package gincontext

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/morehao/golib/biz/gcontext"
	"github.com/morehao/golib/gerror"
)

type DtoRender struct {
	Code      int    `json:"code"`
	RequestID string `json:"requestID"`
	Msg       string `json:"msg"`
	Data      any    `json:"data"`
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
	r.SetRequestID(GetRequestID(ctx))
	r.SetMsg("success")
	if withFormat {
		r.SetDataWithFormat(data)
	} else {
		r.SetData(data)
	}
	ctx.JSON(http.StatusOK, r)
}

func Fail(ctx *gin.Context, err error) {
	r := buildErrorResponse(ctx, err)
	ctx.JSON(http.StatusOK, r)
}

// FailWithStatus 与 Fail 相同，但允许调用方指定 HTTP 状态码。
//
// 存在的原因：本包约定业务错误用 HTTP 200 + 非 0 code 表达，但少数错误需要
// 用状态码本身表达语义（如 409 冲突），以便客户端不经解析响应体就能判断。
// 提供这个函数是为了让"换状态码"与"构造 envelope"解耦 —— 否则调用方会自己
// 拼一份 DtoRender，把 code 默认值、msg 取值等约定复制一份，本包改动时它不会
// 跟着变，形成静默分歧。
func FailWithStatus(ctx *gin.Context, status int, err error) {
	r := buildErrorResponse(ctx, err)
	ctx.JSON(status, r)
}

func Abort(ctx *gin.Context, err error) {
	r := buildErrorResponse(ctx, err)
	ctx.AbortWithStatusJSON(http.StatusOK, r)
}

func buildErrorResponse(ctx *gin.Context, err error) gcontext.ResponseRender {
	r := gcontext.NewResponseRender()
	r.SetRequestID(GetRequestID(ctx))
	code := -1
	msg := ""
	if bizCode, bizMsg, ok := businessError(err); ok {
		code = bizCode
		msg = bizMsg
	} else {
		msg = gerror.Cause(err).Error()
	}
	// 业务错误同时写进 context：访问日志据此记录 app.error.code/message，不再依赖
	// 解析响应体（响应体会被压缩或按采集上限截断）。
	SetAppError(ctx, code, msg)
	r.SetCode(code)
	r.SetMsg(msg)
	r.SetData(gin.H{})
	return r
}

// businessError 从错误链里提取业务错误码与文案。
//
// 必须同时兼容 gerror.Error 的**值**与**指针**两种形态：
//   - errors.As 要求错误链上的动态类型可赋值给目标类型，单向匹配会静默失败；
//   - gerror.Error 的方法集全是值接收者，调用方既可能传值（哨兵值），
//     也可能传 *gerror.Error（如历史上 GetError 返回指针的实现），两者都算业务错误；
//   - gerror 包装错误（newWrapped）实现了 As(*Error)，值形态分支即可命中。
//
// 未命中时返回 ok=false，由调用方回落到 gerror.Cause(err).Error()。
func businessError(err error) (code int, msg string, ok bool) {
	var valueErr gerror.Error
	if errors.As(err, &valueErr) {
		return valueErr.Code, valueErr.Msg, true
	}
	var ptrErr *gerror.Error
	if errors.As(err, &ptrErr) && ptrErr != nil {
		return ptrErr.Code, ptrErr.Msg, true
	}
	return 0, "", false
}
