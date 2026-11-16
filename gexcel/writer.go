package gexcel

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/xuri/excelize/v2"
)

// writeSheet writes header + rows into f/sheet using SetSheetRow (one call per
// row instead of one per cell).
func writeSheet[T any](f *excelize.File, rows []T, cfg writeConfig) error {
	if f == nil {
		return ErrNilFile
	}
	if strings.TrimSpace(cfg.sheet) == "" {
		return ErrSheetRequired
	}
	if cfg.headerRow <= 0 {
		return ErrInvalidRowIndex
	}

	schema, err := buildSchemaForWrite[T](cfg.columns)
	if err != nil {
		return err
	}
	if len(schema) == 0 {
		return ErrNoWritableColumns
	}

	// Ensure the target sheet exists and becomes active; drop the default empty
	// Sheet1 when we create a differently-named sheet.
	if err := ensureSheet(f, cfg.sheet); err != nil {
		return err
	}

	// Header row.
	headerCell, err := excelize.CoordinatesToCellName(1, cfg.headerRow)
	if err != nil {
		return err
	}
	header := make([]any, len(schema))
	for i := range schema {
		header[i] = schema[i].column
	}
	if err := f.SetSheetRow(cfg.sheet, headerCell, &header); err != nil {
		return err
	}

	// Data rows.
	for rowIdx := range rows {
		itemValue, err := derefRow(rows[rowIdx], rowIdx, cfg.nilRowPolicy)
		if err != nil {
			return err
		}
		if !itemValue.IsValid() { // skipped nil row
			continue
		}
		cell, err := excelize.CoordinatesToCellName(1, cfg.headerRow+1+rowIdx)
		if err != nil {
			return err
		}
		values := make([]any, len(schema))
		for col := range schema {
			if schema[col].fieldIndex >= itemValue.NumField() {
				values[col] = nil
				continue
			}
			encoded, encErr := encodeField(itemValue.Field(schema[col].fieldIndex))
			if encErr != nil {
				return fmt.Errorf("row %d field %s: %w", rowIdx, schema[col].fieldName, encErr)
			}
			values[col] = encoded
		}
		if err := f.SetSheetRow(cfg.sheet, cell, &values); err != nil {
			return err
		}
	}

	return nil
}

// buildSchemaForWrite builds the writer schema and applies explicit column
// order from cfg.columns.
func buildSchemaForWrite[T any](columns []string) ([]columnSchema, error) {
	var zero T
	base, err := buildSchema(reflect.TypeOf(zero), nil)
	if err != nil {
		return nil, err
	}
	return orderColumns(base, columns), nil
}

// ensureSheet makes cfg.sheet the active sheet, creating it if needed, and
// removes a leftover empty default "Sheet1" when the target has another name.
func ensureSheet(f *excelize.File, sheet string) error {
	idx, err := f.GetSheetIndex(sheet)
	if err != nil {
		return err
	}
	if idx < 0 {
		if _, err := f.NewSheet(sheet); err != nil {
			return err
		}
		// Remove the default empty Sheet1 if we just created a different sheet.
		if sheet != "Sheet1" {
			if s1, _ := f.GetSheetIndex("Sheet1"); s1 >= 0 {
				if empty, _ := sheetIsEmpty(f, "Sheet1"); empty {
					_ = f.DeleteSheet("Sheet1")
				}
			}
		}
	}
	// Re-resolve index after possible deletion.
	if idx, err = f.GetSheetIndex(sheet); err == nil && idx >= 0 {
		f.SetActiveSheet(idx)
	}
	return nil
}

func sheetIsEmpty(f *excelize.File, sheet string) (bool, error) {
	rows, err := f.GetRows(sheet)
	if err != nil {
		return false, err
	}
	for _, r := range rows {
		if !isEmptyLine(r) {
			return false, nil
		}
	}
	return true, nil
}

// derefRow dereferences a row element (value or pointer) into a struct Value.
// Returns an invalid Value (to skip) when nil and policy is NilRowSkip.
func derefRow[T any](row T, rowIdx int, policy NilRowPolicy) (reflect.Value, error) {
	v := reflect.ValueOf(row)
	for v.Kind() == reflect.Ptr {
		if v.IsNil() {
			switch policy {
			case NilRowSkip:
				return reflect.Value{}, nil
			default:
				return reflect.Value{}, fmt.Errorf("%w: index %d", ErrNilRow, rowIdx)
			}
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return reflect.Value{}, fmt.Errorf("%w: got %s", ErrSchemaNotStruct, v.Kind())
	}
	return v, nil
}

func isEmptyLine(data []string) bool {
	for _, v := range data {
		if strings.TrimSpace(v) != "" {
			return false
		}
	}
	return true
}
