package gexcel

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/xuri/excelize/v2"
)

type fieldBinding struct {
	fieldName  string
	fieldIndex int
	column     string
	colIndex   int
}

func (cfg readConfig) decodeConfig(date1904 bool) decodeConfig {
	return decodeConfig{
		trimSpace:   cfg.trimSpace,
		thousandSep: cfg.thousandSep,
		timeLayouts: cfg.timeLayouts,
		date1904:    date1904,
		emptyAsNil:  cfg.emptyAsNil,
		trueValues:  defaultDecodeConfig().trueValues,
		falseValues: defaultDecodeConfig().falseValues,
	}
}

// readSheet streams rows through the excelize Rows iterator, calling fn per
// decoded row. It returns collected RowErrors and a fatal error if any.
func readSheet[T any](f *excelize.File, cfg readConfig, fn func(T) error) ([]RowError, error) {
	if f == nil {
		return nil, ErrNilFile
	}
	if strings.TrimSpace(cfg.sheet) == "" {
		return nil, ErrSheetRequired
	}
	if cfg.headerRow <= 0 || cfg.dataStartRow <= 0 {
		return nil, ErrInvalidRowIndex
	}
	if cfg.headerRow >= cfg.dataStartRow {
		return nil, ErrInvalidRowOrder
	}

	schema, err := buildSchemaFromType[T]()
	if err != nil {
		return nil, err
	}
	schema = applyReadColumns(schema, cfg.readColumns)

	date1904 := workbookUses1904(f)
	dconf := cfg.decodeConfig(date1904)

	rows, err := f.Rows(cfg.sheet)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSheetNotFound, err)
	}
	defer func() { _ = rows.Close() }()

	opts := excelize.Options{RawCellValue: cfg.rawCellValue}

	rowNum := 0 // last read Excel row number (1-based).
	var headerCells []string
	var bindings []fieldBinding
	var rowErrors []RowError
	headerBuilt := false

	for rows.Next() {
		cells, err := rows.Columns(opts)
		if err != nil {
			return rowErrors, err
		}
		rowNum++ // gaps are emitted as empty rows, so this stays aligned.

		if rowNum < cfg.headerRow {
			continue
		}
		if rowNum == cfg.headerRow {
			headerCells = cells
			continue
		}
		if !headerBuilt {
			b, hErrs, err := buildBindings(schema, headerCells, cfg)
			if err != nil {
				return rowErrors, err
			}
			bindings = b
			rowErrors = append(rowErrors, hErrs...)
			headerBuilt = true
		}
		if rowNum < cfg.dataStartRow {
			continue
		}

		var item T
		itemValue := reflect.ValueOf(&item).Elem()
		hasError := false

		for _, b := range bindings {
			if b.fieldIndex >= itemValue.NumField() {
				continue
			}
			field := itemValue.Field(b.fieldIndex)
			if !field.CanSet() {
				continue
			}
			raw := ""
			if b.colIndex < len(cells) {
				raw = cells[b.colIndex]
			}
			if derr := decodeField(field, raw, dconf); derr != nil {
				re := toRowError(derr, rowNum, b, raw)
				if cfg.rowErrorPolicy == RowErrorFailFast {
					return rowErrors, fmt.Errorf("row %d column %s: %w", rowNum, b.column, derr)
				}
				rowErrors = append(rowErrors, re)
				hasError = true
			}
		}
		if hasError {
			continue
		}
		if isEmptyValue(itemValue) {
			continue
		}
		if fn != nil {
			if err := fn(item); err != nil {
				return rowErrors, err
			}
		}
	}

	if err := rows.Error(); err != nil {
		return rowErrors, err
	}
	if !headerBuilt {
		return rowErrors, ErrHeaderOutOfRange
	}

	return rowErrors, nil
}

// buildBindings maps struct columns to header positions honouring the header
// policy and the (pre-ordered) schema.
func buildBindings(schema []columnSchema, headers []string, cfg readConfig) ([]fieldBinding, []RowError, error) {
	headIdx := make(map[string]int, len(headers))
	var rowErrs []RowError

	for i, h := range headers {
		name := strings.TrimSpace(h)
		if name == "" {
			continue
		}
		if prev, dup := headIdx[name]; dup {
			switch cfg.headerPolicy.Duplicate {
			case ColumnPolicyError:
				return nil, nil, fmt.Errorf("duplicate header column %q at positions %d and %d", name, prev+1, i+1)
			case ColumnPolicyWarn:
				rowErrs = append(rowErrs, RowError{
					Code: RowErrorCodeDuplicateColumn, Severity: SeverityWarning,
					Row: cfg.headerRow, Column: name,
					Message: fmt.Sprintf("duplicate header column %q, using last occurrence", name),
				})
				headIdx[name] = i
			default: // ignore -> last wins
				headIdx[name] = i
			}
			continue
		}
		headIdx[name] = i
	}

	bindings := make([]fieldBinding, 0, len(schema))
	mapped := make(map[string]bool, len(headers))
	for _, item := range schema {
		idx, ok := headIdx[item.column]
		matchedName := item.column
		if !ok {
			for _, alias := range item.aliases {
				alias = strings.TrimSpace(alias)
				if alias == "" {
					continue
				}
				if aliasIdx, exists := headIdx[alias]; exists {
					idx = aliasIdx
					ok = true
					matchedName = alias
					break
				}
			}
		}
		if !ok {
			switch cfg.headerPolicy.Missing {
			case ColumnPolicyError:
				return nil, nil, fmt.Errorf("required column %q not found", item.column)
			case ColumnPolicyWarn:
				rowErrs = append(rowErrs, RowError{
					Code: RowErrorCodeRequiredMissing, Severity: SeverityWarning,
					Row: cfg.headerRow, Column: item.column, Field: item.fieldName,
					Message: fmt.Sprintf("column %q not found, field skipped", item.column),
				})
			}
			continue
		}
		_ = matchedName
		bindings = append(bindings, fieldBinding{
			fieldName:  item.fieldName,
			fieldIndex: item.fieldIndex,
			column:     item.column,
			colIndex:   idx,
		})
		mapped[strings.TrimSpace(headers[idx])] = true
	}

	// Extra header columns (present in sheet, not mapped to any field).
	if cfg.headerPolicy.Extra != ColumnPolicyIgnore {
		mappedFields := make(map[string]struct{}, len(bindings))
		for _, b := range bindings {
			mappedFields[b.fieldName] = struct{}{}
		}
		for name, i := range headIdx {
			if mapped[name] {
				continue
			}
			switch cfg.headerPolicy.Extra {
			case ColumnPolicyError:
				return nil, nil, fmt.Errorf("unexpected column %q in header", name)
			case ColumnPolicyWarn:
				rowErrs = append(rowErrs, RowError{
					Code: RowErrorCodeUnknownColumn, Severity: SeverityWarning,
					Row: cfg.headerRow, Column: name, Value: strings.TrimSpace(headers[i]),
					Message: fmt.Sprintf("column %q has no matching field, ignored", name),
				})
			}
		}
	}

	if len(bindings) == 0 {
		return nil, rowErrs, ErrNoMatchedColumns
	}
	return bindings, rowErrs, nil
}

// applyReadColumns filters and orders the schema by explicit field names.
func applyReadColumns(schema []columnSchema, fields []string) []columnSchema {
	if len(fields) == 0 {
		return schema
	}
	idxByField := make(map[string]int, len(schema))
	for i := range schema {
		idxByField[schema[i].fieldName] = i
	}
	out := make([]columnSchema, 0, len(fields))
	used := make(map[string]struct{}, len(fields))
	for _, f := range fields {
		name := strings.TrimSpace(f)
		if name == "" {
			continue
		}
		idx, ok := idxByField[name]
		if !ok {
			continue
		}
		if _, dup := used[name]; dup {
			continue
		}
		out = append(out, schema[idx])
		used[name] = struct{}{}
	}
	if len(out) == 0 {
		return schema
	}
	return out
}

func toRowError(err error, row int, b fieldBinding, raw string) RowError {
	code := RowErrorCodeTypeMismatch
	var ce *ConvertError
	if errors.As(err, &ce) && ce.Code != "" {
		code = ce.Code
	}
	return RowError{
		Code:     code,
		Severity: SeverityError,
		Row:      row,
		Column:   b.column,
		Field:    b.fieldName,
		Value:    raw,
		Message:  err.Error(),
	}
}

func isEmptyValue(v reflect.Value) bool {
	return v.IsZero()
}

// workbookUses1904 reports whether the workbook uses the 1904 date system.
func workbookUses1904(f *excelize.File) bool {
	// excelize exposes no direct accessor; parse via GetWorkbookProps if present.
	// Fall back to false when unavailable.
	if props, err := f.GetWorkbookProps(); err == nil && props.Date1904 != nil {
		return *props.Date1904
	}
	return false
}
