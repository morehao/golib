package excel

import (
	"bytes"
	"fmt"
	"reflect"
	"sync"

	"github.com/xuri/excelize/v2"
)

type Write struct {
	sheetName string
	headRow   int
	file      *excelize.File
	lock      sync.Mutex
}

type WriterInitOption struct {
	SheetName string // 表名
	HeadRow   int    // 旧 API：0-based；v2 WriteOption 使用 1-based。
}

// NewWrite 使用旧初始化入口，HeadRow 按 0-based 解释。
func NewWrite(option *WriterInitOption) *Write {
	if option == nil {
		return nil
	}
	file := excelize.NewFile()
	return &Write{
		sheetName: option.SheetName,
		headRow:   option.HeadRow,
		file:      file,
	}
}

func WriterInitOptionFromWriteOptions(options ...WriteOption) *WriterInitOption {
	cfg := defaultWriteConfig()
	for _, opt := range options {
		if opt == nil {
			continue
		}
		opt(&cfg)
	}

	headRow := cfg.headerRow - 1

	return &WriterInitOption{
		SheetName: cfg.sheet,
		HeadRow:   headRow,
	}
}

func NewWriteWithOptions(options ...WriteOption) *Write {
	return NewWrite(WriterInitOptionFromWriteOptions(options...))
}

func (w *Write) SaveAs(data interface{}, filePath string) error {
	_, err := w.GenerateFileStream(data)
	if err != nil {
		return err
	}
	return w.file.SaveAs(filePath)
}

func (w *Write) GenerateFileStream(data interface{}) (*bytes.Buffer, error) {
	w.lock.Lock()
	defer w.lock.Unlock()

	dataValue := reflect.ValueOf(data)
	if dataValue.Kind() != reflect.Slice {
		return nil, fmt.Errorf("data must be a slice")
	}

	// 创建新表
	if err := w.newSheet(w.sheetName); err != nil {
		return nil, err
	}

	// 获取头行数据
	var headRows []string
	var colMap map[string]int
	if dataValue.Len() == 0 {
		// 创建一个新实例以获取头行数据
		elemType := dataValue.Type().Elem()
		tempInstance := reflect.New(elemType).Elem().Interface()
		headRows, colMap = w.getHeadRowsAndColMap(tempInstance)
	} else {
		headRows, colMap = w.getHeadRowsAndColMap(dataValue.Index(0).Interface())
	}

	// 写入头行数据
	for colIdx, head := range headRows {
		cell, transErr := excelize.CoordinatesToCellName(colIdx+1, w.headRow+1)
		if transErr != nil {
			return nil, transErr
		}
		if err := w.file.SetCellValue(w.sheetName, cell, head); err != nil {
			return nil, err
		}
	}

	// 检查切片是否为空
	if dataValue.Len() == 0 {
		return w.getBuffer()
	}

	// 遍历切片元素并写入数据
	for i := 0; i < dataValue.Len(); i++ {
		elementValue := dataValue.Index(i)
		if elementValue.Kind() != reflect.Struct {
			return nil, fmt.Errorf("data must be a slice of structs")
		}
		// 写入每个元素的数据
		if err := w.writeRow(i, elementValue.Interface(), colMap); err != nil {
			return nil, err
		}
	}

	return w.getBuffer()
}

func (w *Write) getBuffer() (*bytes.Buffer, error) {
	var buffer bytes.Buffer
	if err := w.file.Write(&buffer); err != nil {
		return nil, err
	}
	return &buffer, nil
}

func (w *Write) getHeadRowsAndColMap(elem interface{}) ([]string, map[string]int) {
	var headRows []string
	colMap := make(map[string]int)
	elemValue := reflect.ValueOf(elem)
	elemType := elemValue.Type()
	for i := 0; i < elemType.NumField(); i++ {
		field := elemType.Field(i)
		tagValue := field.Tag.Get(tagExcel)
		if tagValue == "" {
			continue
		}
		headTag := getHeadTag(tagValue)
		if headTag == nil || headTag.param == "" {
			continue
		}
		headRows = append(headRows, headTag.param)
		colMap[headTag.param] = i
	}
	return headRows, colMap
}

func (w *Write) newSheet(sheetName string) error {
	index, err := w.file.NewSheet(sheetName)
	if err != nil {
		return err
	}
	w.file.SetActiveSheet(index)
	return nil
}

func (w *Write) writeRow(rowIndex int, elem interface{}, colMap map[string]int) error {
	elemValue := reflect.ValueOf(elem)
	for _, colIdx := range colMap {
		fieldValue := elemValue.Field(colIdx)
		cell, transErr := excelize.CoordinatesToCellName(colIdx+1, w.headRow+2+rowIndex)
		if transErr != nil {
			return transErr
		}
		if err := w.file.SetCellValue(w.sheetName, cell, fieldValue.Interface()); err != nil {
			return err
		}
	}
	return nil
}
