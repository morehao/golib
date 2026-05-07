package excel

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/xuri/excelize/v2"
)

type fieldBinding struct {
	fieldIndex int
	column     string
	colIndex   int
}

func readRows[T any](f *excelize.File, cfg readConfig) ([]T, []RowError, error) {
	if f == nil {
		return nil, nil, fmt.Errorf("excel file is nil")
	}
	if cfg.sheet == "" {
		return nil, nil, fmt.Errorf("sheet is required")
	}
	if cfg.headerRow <= 0 {
		return nil, nil, fmt.Errorf("headerRow must be >= 1")
	}
	if cfg.dataStartRow <= 0 {
		return nil, nil, fmt.Errorf("dataStartRow must be >= 1")
	}
	if cfg.headerRow >= cfg.dataStartRow {
		return nil, nil, fmt.Errorf("headerRow must be less than dataStartRow")
	}

	rawRows, err := f.GetRows(cfg.sheet)
	if err != nil {
		return nil, nil, err
	}
	if cfg.headerRow > len(rawRows) {
		return nil, nil, fmt.Errorf("headerRow out of range")
	}

	schema, err := buildSchemaFromType[T]()
	if err != nil {
		return nil, nil, err
	}

	headers := rawRows[cfg.headerRow-1]
	headIdx := make(map[string]int, len(headers))
	for i, h := range headers {
		name := strings.TrimSpace(h)
		if name == "" {
			continue
		}
		headIdx[name] = i
	}

	bindings := make([]fieldBinding, 0, len(schema))
	for _, item := range schema {
		idx, ok := headIdx[item.column]
		if !ok {
			for _, alias := range item.aliases {
				alias = strings.TrimSpace(alias)
				if alias == "" {
					continue
				}
				if aliasIdx, exists := headIdx[alias]; exists {
					idx = aliasIdx
					ok = true
					break
				}
			}
		}

		if !ok {
			if cfg.strictHeader {
				return nil, nil, fmt.Errorf("required column %q not found", item.column)
			}
			continue
		}

		bindings = append(bindings, fieldBinding{
			fieldIndex: item.fieldIndex,
			column:     item.column,
			colIndex:   idx,
		})
	}

	if len(bindings) == 0 {
		return nil, nil, fmt.Errorf("excel: no matched columns in header")
	}

	result := make([]T, 0)
	rowErrors := make([]RowError, 0)

	for rowIdx := cfg.dataStartRow - 1; rowIdx < len(rawRows); rowIdx++ {
		cells := rawRows[rowIdx]
		if isEmptyLine(cells) {
			continue
		}

		var item T
		itemValue := reflect.ValueOf(&item).Elem()
		hasRowError := false

		for _, binding := range bindings {
			if binding.fieldIndex >= itemValue.NumField() {
				continue
			}
			field := itemValue.Field(binding.fieldIndex)
			if !field.CanSet() {
				continue
			}

			raw := ""
			if binding.colIndex < len(cells) {
				raw = cells[binding.colIndex]
			}

			if setErr := setFieldFromString(field, raw); setErr != nil {
				var convErr *ConvertError
				if errors.As(setErr, &convErr) {
					code := convErr.Code
					if code == "" {
						code = RowErrorCodeTypeMismatch
					}
					rowErrors = append(rowErrors, RowError{
						Code:    code,
						Row:     rowIdx + 1,
						Column:  binding.column,
						Value:   raw,
						Message: setErr.Error(),
					})
					hasRowError = true
					continue
				}
				return nil, nil, setErr
			}
		}

		if hasRowError {
			continue
		}
		result = append(result, item)
	}

	return result, rowErrors, nil
}

func isEmptyLine(data []string) bool {
	for _, v := range data {
		if strings.TrimSpace(v) != "" {
			return false
		}
	}
	return true
}
