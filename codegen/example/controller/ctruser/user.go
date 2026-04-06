package ctruser

import (
	"fmt"
	"strconv"

	"github.com/morehao/golib/codegen/example/dto/dtouser"
)

func UserSetting(req dtouser.UserSettingReq) dtouser.UserSettingRes {
	fmt.Println("test")
	fmt.Println(strconv.Itoa(1))
	return dtouser.UserSettingRes{}
}
