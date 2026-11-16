package gexcel

import "errors"

const (
	RowErrorCodeTypeMismatch    = "type_mismatch"
	RowErrorCodeRequiredMissing = "required_missing"
	RowErrorCodeColumnNotFound  = "column_not_found"
	RowErrorCodeDuplicateColumn = "duplicate_column"
	RowErrorCodeUnknownColumn   = "unknown_column"
)

// Severity classifies a RowError.
type Severity int

const (
	// SeverityError means the row was dropped (or aborted) because of this.
	SeverityError Severity = iota
	// SeverityWarning means the issue was noted but the row was kept.
	SeverityWarning
)

func (s Severity) String() string {
	switch s {
	case SeverityWarning:
		return "warning"
	default:
		return "error"
	}
}

// RowError reports a per-row (or per-column) decoding problem.
type RowError struct {
	Code     string
	Severity Severity
	Row      int    // 1-based Excel row number.
	Column   string // column header name.
	Field    string // struct field name, when known.
	Value    string // raw cell value.
	Message  string
}

func (e RowError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return e.Code
}

// Package-level sentinel errors, usable with errors.Is.
var (
	ErrNilFile           = errors.New("gexcel: excel file is nil")
	ErrSheetRequired     = errors.New("gexcel: sheet is required")
	ErrSheetNotFound     = errors.New("gexcel: sheet not found")
	ErrInvalidRowIndex   = errors.New("gexcel: row index must be >= 1")
	ErrInvalidRowOrder   = errors.New("gexcel: headerRow must be less than dataStartRow")
	ErrHeaderOutOfRange  = errors.New("gexcel: headerRow out of range")
	ErrNoMatchedColumns  = errors.New("gexcel: no matched columns in header")
	ErrInvalidTag        = errors.New("gexcel: invalid excel tag")
	ErrUnsupportedType   = errors.New("gexcel: unsupported field type")
	ErrNoWritableColumns = errors.New("gexcel: no writable columns in schema")
	ErrNilRow            = errors.New("gexcel: row is nil")
	ErrSchemaNotStruct   = errors.New("gexcel: schema type must be a struct")
)
