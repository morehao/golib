package main

import (
	"fmt"

	"github.com/morehao/golib/gexcel"
)

func main() {
	path := "write.xlsx"
	if err := write(path); err != nil {
		fmt.Println("write error:", err)
		return
	}

	if err := read(path); err != nil {
		fmt.Println("read error:", err)
	}
}

type DataItem struct {
	SerialNumber int64  `excel:"col=序号"`
	UserName     string `excel:"col=姓名"`
	Age          int64  `excel:"col=年龄"`
}

func read(path string) error {
	dataList, rowErrs, err := gexcel.ReadFile[DataItem](path)
	if err != nil {
		return err
	}
	if len(rowErrs) > 0 {
		fmt.Println("row errors:", rowErrs)
	}

	for _, item := range dataList {
		fmt.Println(item)
	}

	return nil
}

func write(path string) error {
	dataList := []DataItem{{
		SerialNumber: 1,
		UserName:     "张三",
		Age:          18,
	}, {
		SerialNumber: 2,
		UserName:     "李四",
		Age:          22,
	}}

	return gexcel.WriteFile(dataList, path)
}
