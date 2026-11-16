package gexcel

const (
	RowErrorCodeTypeMismatch    = "type_mismatch"
	RowErrorCodeRequiredMissing = "required_missing"
	RowErrorCodeColumnNotFound  = "column_not_found"
	RowErrorCodeDuplicateColumn = "duplicate_column"
	RowErrorCodeUnknownColumn   = "unknown_column"
)

type RowError struct {
	Code    string
	Row     int
	Column  string
	Value   string
	Message string
}
