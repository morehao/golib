package excel

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/xuri/excelize/v2"
)

func writeWorkbook[T any](f *excelize.File, rows []T, cfg writeConfig) error {
	if f == nil {
		return fmt.Errorf("excel file is nil")
	}
	if cfg.sheet == "" {
		return fmt.Errorf("sheet is required")
	}
	if cfg.headerRow <= 0 {
		return fmt.Errorf("headerRow must be >= 1")
	}

	schema, err := schemaForWrite[T](cfg.columns)
	if err != nil {
		return err
	}
	if len(schema) == 0 {
		return fmt.Errorf("excel: no writable columns in schema")
	}

	sheetIndex := -1
	for idx, name := range f.GetSheetList() {
		if name == cfg.sheet {
			sheetIndex = idx
			break
		}
	}
	if sheetIndex < 0 {
		index, newErr := f.NewSheet(cfg.sheet)
		if newErr != nil {
			return newErr
		}
		sheetIndex = index
	}
	f.SetActiveSheet(sheetIndex)

	for col := range schema {
		cell, cellErr := excelize.CoordinatesToCellName(col+1, cfg.headerRow)
		if cellErr != nil {
			return cellErr
		}
		if setErr := f.SetCellValue(cfg.sheet, cell, schema[col].column); setErr != nil {
			return setErr
		}
	}

	for rowIdx := range rows {
		itemValue := reflect.ValueOf(rows[rowIdx])
		for itemValue.Kind() == reflect.Ptr {
			if itemValue.IsNil() {
				return fmt.Errorf("row %d is nil", rowIdx)
			}
			itemValue = itemValue.Elem()
		}
		if itemValue.Kind() != reflect.Struct {
			return fmt.Errorf("data must be a slice of structs")
		}

		for col := range schema {
			cell, cellErr := excelize.CoordinatesToCellName(col+1, cfg.headerRow+1+rowIdx)
			if cellErr != nil {
				return cellErr
			}
			if setErr := f.SetCellValue(cfg.sheet, cell, itemValue.Field(schema[col].fieldIndex).Interface()); setErr != nil {
				return setErr
			}
		}
	}

	return nil
}

func schemaForWrite[T any](columns []string) ([]columnSchema, error) {
	var zero T
	typ := reflect.TypeOf(zero)
	for typ != nil && typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}
	if typ == nil || typ.Kind() != reflect.Struct {
		return nil, fmt.Errorf("schema type must be a struct")
	}

	base, err := buildSchema(typ, nil)
	if err != nil {
		return nil, err
	}
	if len(columns) == 0 {
		return base, nil
	}

	idxByField := make(map[string]int, len(base))
	for i := range base {
		idxByField[base[i].fieldName] = i
	}

	ordered := make([]columnSchema, 0, len(base))
	used := make(map[string]struct{}, len(base))
	for _, field := range columns {
		fieldName := strings.TrimSpace(field)
		if fieldName == "" {
			continue
		}
		idx, ok := idxByField[fieldName]
		if !ok {
			continue
		}
		if _, exists := used[fieldName]; exists {
			continue
		}
		ordered = append(ordered, base[idx])
		used[fieldName] = struct{}{}
	}

	for i := range base {
		if _, exists := used[base[i].fieldName]; exists {
			continue
		}
		ordered = append(ordered, base[i])
	}

	return ordered, nil
}
