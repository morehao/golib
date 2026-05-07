package excel

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSaveAs(t *testing.T) {
	type Dest struct {
		SerialNumber int64  `ex:"head:序号" validate:"min=10,max=100"`
		UserName     string `ex:"head:姓名"`
		Age          int64  `ex:"head:年龄"`
	}
	var dataList []Dest
	dataList = append(dataList, Dest{
		SerialNumber: 1,
		UserName:     "张三",
		Age:          18,
	})
	excelWriter := NewWrite(&WriterInitOption{
		SheetName: "Sheet1",
		HeadRow:   0,
	})
	err := excelWriter.SaveAs(dataList, "write.xlsx")
	assert.Nil(t, err)
}
