//go:build ignore

// ddlgen 命令：打印各组件的 MySQL / PostgreSQL 建表 DDL，用于同步各包 README 的
// 「手工建表 DDL」章节。生成逻辑在 internal/ddlgen 包，这里只做命令行入口。
//
// 用法（在仓库根目录执行）：
//
//	go run internal/ddlgen/main.go            # 全部包、两种方言
//	go run internal/ddlgen/main.go dict       # 只生成 dict
//
// 各 README 的 DDL 由 internal/ddlgen/readme_test.go 在测试时逐语句校验，
// 改了 gorm tag 后忘了同步会直接测试失败。
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/morehao/golib/internal/ddlgen"
)

func main() {
	filter := ""
	if len(os.Args) > 1 {
		filter = os.Args[1]
	}
	if filter != "" {
		if _, ok := ddlgen.Lookup(filter); !ok {
			fmt.Fprintf(os.Stderr, "ddlgen: unknown group %q, want one of %s\n",
				filter, strings.Join(ddlgen.GroupNames(), ", "))
			os.Exit(2)
		}
	}
	for _, g := range ddlgen.Groups {
		if filter != "" && filter != g.Name {
			continue
		}
		fmt.Printf("===== %s =====\n", g.Name)
		for _, dialect := range ddlgen.Dialects {
			stmts, err := ddlgen.Generate(dialect, g.Models)
			if err != nil {
				fmt.Fprintf(os.Stderr, "ddlgen: %s/%s: %v\n", g.Name, dialect, err)
				os.Exit(1)
			}
			fmt.Printf("----- %s -----\n%s\n\n", dialect, strings.Join(stmts, "\n\n"))
		}
	}
}
