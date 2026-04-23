package gast

const (
	UserCreateErr      = 100100
	UserDeleteErr      = 100101
	UserUpdateErr      = 100102
	UserGetDetailErr   = 100103
	UserGetPageListErr = 100104
	UserNotExistErr    = 100105
	UserLoginErr       = 100001
	TableNameUser      = "user"
)

var userErrorMsgMap = map[int]string{
	UserCreateErr:      "创建用户失败",
	UserDeleteErr:      "删除用户失败",
	UserUpdateErr:      "修改用户失败",
	UserGetDetailErr:   "查看用户失败",
	UserGetPageListErr: "查看用户列表失败", UserLoginErr: "用户登录失败", UserLoginErr: "用户登录失败",
}
