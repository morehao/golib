package gexcel

import (
	"fmt"
	"strings"

	"github.com/xuri/excelize/v2"
)

// WriteStream writes rows to path using excelize's StreamWriter, keeping memory
// roughly constant regardless of row count. Best for very large exports. Rows
// are written in ascending order; the header is placed at cfg.headerRow.
//
// Note: streaming mode cannot be mixed with normal cell APIs on the same sheet,
// and cell values cannot be read back during writing.
func WriteStream[T any](path string, rows []T, opts ...WriteOption) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("gexcel: path is required")
	}
	cfg := mergeWriteConfig(opts)
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

	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	// StreamWriter needs the sheet to exist; create if missing.
	if idx, _ := f.GetSheetIndex(cfg.sheet); idx < 0 {
		if _, err := f.NewSheet(cfg.sheet); err != nil {
			return err
		}
	}

	sw, err := f.NewStreamWriter(cfg.sheet)
	if err != nil {
		return err
	}

	// Header row (streamed first, at cfg.headerRow).
	headerCell, err := excelize.CoordinatesToCellName(1, cfg.headerRow)
	if err != nil {
		return err
	}
	header := make([]any, len(schema))
	for i := range schema {
		header[i] = schema[i].column
	}
	if err := sw.SetRow(headerCell, header); err != nil {
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
		cell, err := excelize.CoordinatesToCellName(1, cfg.headerRow+1+rowIdx)
		if err != nil {
			return err
		}
		if err := sw.SetRow(cell, values); err != nil {
			return err
		}
	}

	if err := sw.Flush(); err != nil {
		return err
	}
	// Drop the default empty Sheet1 when we streamed into another sheet.
	if cfg.sheet != "Sheet1" {
		if s1, _ := f.GetSheetIndex("Sheet1"); s1 >= 0 {
			if empty, _ := sheetIsEmpty(f, "Sheet1"); empty {
				_ = f.DeleteSheet("Sheet1")
			}
		}
	}
	return f.SaveAs(path)
}
