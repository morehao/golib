package ctr{{.PackageName}}

import (
	"fmt"
	"strconv"
)

func {{.FunctionName}}(req {{.FunctionName}}Req) {{.FunctionName}}Res {
	fmt.Println("test")
	fmt.Println(strconv.Itoa(1))
}
