package excel

const (
	RowErrorCodeTypeMismatch   = "type_mismatch"
	RowErrorCodeRequiredMissing = "required_missing"
	RowErrorCodeColumnNotFound = "column_not_found"
)

type RowError struct {
	Code    string
	Row     int
	Column  string
	Value   string
	Message string
}
