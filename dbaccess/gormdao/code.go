package genericdao

import (
	"github.com/morehao/golib/gerror"
)

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

func getDBError(code int) *gerror.Error {
	return &gerror.Error{
		Code: code,
		Msg:  DBErrorMsgMap[code],
	}
}
