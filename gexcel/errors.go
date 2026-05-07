package gexcel

const (
	RowErrorCodeTypeMismatch   = "type_mismatch"
	RowErrorCodeRequiredMissing = "required_missing"
	// Deprecated: use RowErrorCodeRequiredMissing.
	RowErrorCodeRequiredMiss   = RowErrorCodeRequiredMissing
	RowErrorCodeColumnNotFound = "column_not_found"
)

type RowError struct {
	Code    string
	Row     int
	Column  string
	Value   string
	Message string
}
