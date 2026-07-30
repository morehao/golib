package gconstant

import "github.com/morehao/golib/gerror"

// 数据库相关错误码 (100000-100099)
// 注意：DB相关错误是内部错误，前端不感知，不应直接返回给前端
const (
	DBInsertErr = 100000
	DBDeleteErr = 100001
	DBUpdateErr = 100002
	DBFindErr   = 100003
)

var DBErrorMsgMap = gerror.CodeMsgMap{
	DBInsertErr: "db insert error",
	DBDeleteErr: "db delete error",
	DBUpdateErr: "db update error",
	DBFindErr:   "db find error",
}

// 系统相关错误码 (100100-100199)，前端需要感知的系统错误，不可随意更改
const (
	ParamInvalidErr = 100104
	SystemErrorErr  = 100105
)

var SystemErrorMsgMap = gerror.CodeMsgMap{
	ParamInvalidErr: "invalid parameter",
	SystemErrorErr:  "system error",
}

// 权限/认证相关错误码 (110020-110029)
const (
	UnauthorizedErr     = 110000
	ForbiddenErr        = 110001
	TokenInvalidErr     = 110002
	TokenExpiredErr     = 110003
	PermissionDeniedErr = 110004
)

var AuthErrorMsgMap = gerror.CodeMsgMap{
	UnauthorizedErr:     "unauthorized",
	ForbiddenErr:        "forbidden",
	TokenInvalidErr:     "invalid token",
	TokenExpiredErr:     "token expired",
	PermissionDeniedErr: "permission denied",
}
