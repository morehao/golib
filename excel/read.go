package excel

import (
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync"

	"github.com/go-playground/locales/zh_Hans_CN"
	ut "github.com/go-playground/universal-translator"
	"github.com/go-playground/validator/v10"
	zhTrans "github.com/go-playground/validator/v10/translations/zh"
	"github.com/xuri/excelize/v2"
)

type Reader struct {
	file         *excelize.File
	sheetName    string
	headRow      int
	dataStartRow int
	lock         sync.Mutex
}

type ReaderOption struct {
	SheetNumber  int // 0开始
	HeadRow      int // 旧 API：0-based；v2 ReadOption 使用 1-based。
	DataStartRow int // 旧 API：0-based；v2 ReadOption 使用 1-based。
}

func NewReader(file *excelize.File, option *ReaderOption) *Reader {
	if file == nil || option == nil {
		return nil
	}
	return &Reader{
		file:         file,
		sheetName:    file.GetSheetName(option.SheetNumber),
		headRow:      option.HeadRow,
		dataStartRow: option.DataStartRow,
	}
}

func (r *Reader) Read(dest interface{}) (map[int][]ValidationError, error) {
	r.lock.Lock()
	defer r.lock.Unlock()
	if r.headRow >= r.dataStartRow {
		return nil, errors.New("head row must be less than data start row")
	}
	rows, getRowsErr := r.file.GetRows(r.sheetName)
	if getRowsErr != nil {
		return nil, getRowsErr
	}
	if len(rows) == 0 {
		return nil, errors.New("empty sheet")
	}
	if len(rows) <= r.dataStartRow {
		return nil, errors.New("no data")
	}

	headRows := rows[r.headRow]
	dataRows := rows[r.dataStartRow:]

	headMap := r.getHeadMap(headRows)
	if len(headMap) == 0 {
		return nil, errors.New("empty head")
	}
	validateErrMap := make(map[int][]ValidationError)
	bindValidateErrMap, bindErr := r.bindDataToDest(headMap, dataRows, dest)
	if bindErr != nil {
		return nil, bindErr
	}
	for dataRowNumber, errList := range bindValidateErrMap {
		validateErrMap[dataRowNumber] = append(validateErrMap[dataRowNumber], errList...)
	}

	dataValidateErrMap, validateErr := r.validateData(dest)
	if validateErr != nil {
		return nil, validateErr
	}
	for dataRowNumber, errList := range dataValidateErrMap {
		validateErrMap[dataRowNumber] = append(validateErrMap[dataRowNumber], errList...)
	}
	return validateErrMap, nil
}

func (r *Reader) getHeadMap(headRows []string) map[string]int {
	headMap := make(map[string]int)
	for i, cell := range headRows {
		if cell == "" {
			continue
		}
		headMap[cell] = i
	}
	return headMap
}

func (r *Reader) bindDataToDest(headMap map[string]int, dataRows [][]string, dest interface{}) (map[int][]ValidationError, error) {
	destValue := reflect.ValueOf(dest)
	if destValue.Kind() != reflect.Ptr || destValue.Elem().Kind() != reflect.Slice {
		return nil, errors.New("dest must be a pointer to a slice")
	}
	elemType := destValue.Elem().Type().Elem()
	formatDataList := make([]reflect.Value, 0)

	destValidateErrMap := make(map[int][]ValidationError)

	for dataRowNumber, dataList := range dataRows {
		if isEmptyLine(dataList) {
			continue
		}
		item := reflect.New(elemType).Elem()
		stValidateErrMap, bindErr := r.bindDataToSt(dataRowNumber, dataList, headMap, item)
		if bindErr != nil {
			return nil, bindErr
		}
		for k, errList := range stValidateErrMap {
			destValidateErrMap[k] = append(destValidateErrMap[dataRowNumber], errList...)
		}
		formatDataList = append(formatDataList, item)
	}
	destValue.Elem().Set(reflect.Append(destValue.Elem(), formatDataList...))
	return destValidateErrMap, nil
}

func (r *Reader) bindDataToSt(dataRowNumber int, dataList []string, headMap map[string]int, stValue reflect.Value) (map[int][]ValidationError, error) {
	if stValue.Kind() != reflect.Struct {
		return nil, errors.New("[bindDataToSt] stValue must be a struct")
	}
	validateErrMap := make(map[int][]ValidationError)

	for i := 0; i < stValue.NumField(); i++ {
		field := stValue.Field(i)
		structField := stValue.Type().Field(i)
		if structField.Anonymous {
			return r.bindDataToSt(dataRowNumber, dataList, headMap, field)
		}
		tagValue := structField.Tag.Get(tagExcel)
		if tagValue == "" {
			continue
		}
		subTagMap := getSubTagMap(tagValue)
		headTag, headTagExist := subTagMap[subTagHead]
		if !headTagExist || headTag.param == "" {
			return nil, fmt.Errorf("head tag not found for field %s", structField.Name)
		}
		headIndex, headExist := headMap[headTag.param]
		if !headExist || headIndex >= len(dataList) {
			continue
		}
		value := strings.TrimSpace(dataList[headIndex])
		if err := checkFieldTypes(structField.Type.Kind(), headTag.param, value); err != nil {
			validateErrMap[dataRowNumber] = append(validateErrMap[i], ValidationError{
				DataRowNumber: dataRowNumber,
				Head:          headTag.param,
				CellValue:     value,
				ExpectType:    structField.Type.Kind().String(),
				ErrorMessage:  err.Error(),
			})
		}
		if err := setFieldValue(field.Kind(), value, field, headTag.tag); err != nil {
			return nil, err
		}
	}
	return validateErrMap, nil
}

func (r *Reader) validateData(data interface{}) (map[int][]ValidationError, error) {
	validate := validator.New()
	zh := zh_Hans_CN.New()
	uni := ut.New(zh, zh)
	trans, _ := uni.GetTranslator("zh_Hans_CN")
	_ = zhTrans.RegisterDefaultTranslations(validate, trans)

	validate.RegisterTagNameFunc(func(fld reflect.StructField) string {
		tag := fld.Tag.Get(tagExcel)
		if tag == "" {
			return fld.Name
		}
		subTagMap := getSubTagMap(tag)
		headTag, headExist := subTagMap[subTagHead]
		if !headExist || headTag.param == "" {
			return fld.Name
		}
		return headTag.param
	})

	validateErrMap := make(map[int][]ValidationError)
	destValue := reflect.ValueOf(data).Elem()
	for i := 0; i < destValue.Len(); i++ {
		item := destValue.Index(i).Interface()
		if err := validate.Struct(item); err != nil {
			var validationErrors validator.ValidationErrors
			if errors.As(err, &validationErrors) {
				for _, v := range validationErrors {
					errMsg := v.Translate(trans)
					validateErrMap[i] = append(validateErrMap[i], ValidationError{
						DataRowNumber: i,
						Head:          v.Field(),
						CellValue:     fmt.Sprintf("%v", v.Value()),
						ErrorMessage:  errMsg,
					})
				}
			} else {
				return nil, err
			}
		}
	}
	return validateErrMap, nil
}

// checkFieldTypes 检查 Excel 数据类型是否符合预期
func checkFieldTypes(kind reflect.Kind, head, value string) error {
	if head == "" {
		return nil
	}

	switch kind {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint32, reflect.Uint64:
		// 如果有逗号，去掉逗号
		newValue := strings.Replace(value, ",", "", -1)
		if _, err := strconv.ParseInt(newValue, 10, 64); err != nil {
			return fmt.Errorf("field %s: expected int64", head)
		}
	case reflect.Float32, reflect.Float64:
		// 如果有逗号，去掉逗号
		newValue := strings.Replace(value, ",", "", -1)
		if _, err := strconv.ParseFloat(newValue, 64); err != nil {
			return fmt.Errorf("field %s: expected float64", head)
		}
	}
	return nil
}

// setFieldValue 根据类型设置字段值
func setFieldValue(kind reflect.Kind, value string, field reflect.Value, key string) error {
	switch kind {
	case reflect.String:
		field.Set(reflect.ValueOf(value))
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint32, reflect.Uint64:

		// 如果有逗号，去掉逗号
		value = strings.Replace(value, ",", "", -1)
		uintVal, _ := strconv.ParseUint(value, 10, 64)

		integerKind := kind
		switch integerKind {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			field.SetInt(int64(uintVal))
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			field.SetUint(uintVal)
		}
	case reflect.Float32, reflect.Float64:
		value = strings.Replace(value, ",", "", -1)
		floatVal, _ := strconv.ParseFloat(value, 64)
		field.SetFloat(floatVal)
	default:
		return fmt.Errorf("field type not support, key: %s", key)
	}
	return nil
}

func isEmptyLine(data []string) bool {
	for _, v := range data {
		if len(strings.TrimSpace(v)) != 0 {
			return false
		}
	}
	return true
}
