package excel

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/xuri/excelize/v2"
)

func TestRead(t *testing.T) {
	f, err := excelize.OpenFile("read.xlsx")
	assert.Nil(t, err)
	type Dest struct {
		SerialNumber int64  `ex:"head:序号" validate:"min=10,max=100"`
		UserName     string `ex:"head:姓名"`
		Age          int64  `ex:"head:年龄"`
	}
	var dataList []Dest
	excelReader := NewReader(f, &ReaderOption{
		SheetNumber:  0,
		HeadRow:      0,
		DataStartRow: 1,
	})
	validateErrMap, readerErr := excelReader.Read(&dataList)
	assert.Nil(t, readerErr)
	resBytes, _ := json.Marshal(dataList)
	res := string(resBytes)
	fmt.Println(res)
	errMapBytes, _ := json.Marshal(validateErrMap)
	errMap := string(errMapBytes)
	fmt.Println(errMap)
}

func TestReadHeadRowOutOfRangeReturnsError(t *testing.T) {
	f := excelize.NewFile()
	defaultSheet := f.GetSheetName(0)
	f.SetSheetRow(defaultSheet, "A1", &[]interface{}{"name"})
	f.SetSheetRow(defaultSheet, "A2", &[]interface{}{"alice"})

	type Dest struct {
		Name string `ex:"head:name"`
	}

	reader := NewReader(f, &ReaderOption{
		SheetNumber:  0,
		HeadRow:      -1,
		DataStartRow: 1,
	})

	var data []Dest
	_, err := reader.Read(&data)
	assert.Error(t, err)
	assert.EqualError(t, err, "head row out of range")
}
