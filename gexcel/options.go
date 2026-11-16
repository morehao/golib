package gexcel

// ColumnPolicy controls how a header/column mismatch is handled.
type ColumnPolicy int

const (
	// ColumnPolicyIgnore silently skips the mismatch.
	ColumnPolicyIgnore ColumnPolicy = iota
	// ColumnPolicyWarn records a warning-level RowError but keeps going.
	ColumnPolicyWarn
	// ColumnPolicyError aborts the whole read with an error.
	ColumnPolicyError
)

// HeaderPolicy decides how missing / duplicate / extra header columns are
// treated during a read.
type HeaderPolicy struct {
	// Missing applies when a struct field's column (or alias) is absent from
	// the sheet header.
	Missing ColumnPolicy
	// Duplicate applies when the same header name appears more than once.
	Duplicate ColumnPolicy
	// Extra applies when the sheet header has a column not mapped to any field.
	Extra ColumnPolicy
}

// defaultHeaderPolicy keeps backward-compatible behaviour: missing columns are
// skipped, duplicate headers resolve to the last occurrence, extra columns are
// ignored.
func defaultHeaderPolicy() HeaderPolicy {
	return HeaderPolicy{
		Missing:   ColumnPolicyIgnore,
		Duplicate: ColumnPolicyIgnore,
		Extra:     ColumnPolicyIgnore,
	}
}

// RowErrorPolicy decides what happens when a data row fails to decode.
type RowErrorPolicy int

const (
	// RowErrorCollect keeps decoding and appends a RowError per bad row.
	RowErrorCollect RowErrorPolicy = iota
	// RowErrorFailFast stops at the first bad row and returns it as an error.
	RowErrorFailFast
)

// NilRowPolicy decides what happens when a write slice contains a nil element.
type NilRowPolicy int

const (
	// NilRowError returns an error naming the offending row index.
	NilRowError NilRowPolicy = iota
	// NilRowSkip writes an empty row.
	NilRowSkip
)

type readConfig struct {
	sheet        string
	headerRow    int // 1-based row number in Excel.
	dataStartRow int // 1-based row number in Excel.

	rawCellValue bool
	trimSpace    bool
	thousandSep  rune
	timeLayouts  []string
	emptyAsNil   bool

	headerPolicy   HeaderPolicy
	rowErrorPolicy RowErrorPolicy
	readColumns    []string
}

type writeConfig struct {
	sheet        string
	headerRow    int // 1-based row number in Excel.
	columns      []string
	nilRowPolicy NilRowPolicy
	timeLayouts  []string
}

type ReadOption func(*readConfig)

type WriteOption func(*writeConfig)

func defaultReadConfig() readConfig {
	return readConfig{
		sheet:          "Sheet1",
		headerRow:      1,
		dataStartRow:   2,
		trimSpace:      true,
		headerPolicy:   defaultHeaderPolicy(),
		rowErrorPolicy: RowErrorCollect,
	}
}

func defaultWriteConfig() writeConfig {
	return writeConfig{
		sheet:        "Sheet1",
		headerRow:    1,
		nilRowPolicy: NilRowError,
	}
}

func WithReadSheet(sheet string) ReadOption {
	return func(cfg *readConfig) { cfg.sheet = sheet }
}

func WithWriteSheet(sheet string) WriteOption {
	return func(cfg *writeConfig) { cfg.sheet = sheet }
}

func WithHeaderRow(row int) ReadOption {
	// row indexes are 1-based.
	return func(cfg *readConfig) { cfg.headerRow = row }
}

func WithWriteHeaderRow(row int) WriteOption {
	// row indexes are 1-based.
	return func(cfg *writeConfig) { cfg.headerRow = row }
}

func WithDataStartRow(row int) ReadOption {
	// row indexes are 1-based.
	return func(cfg *readConfig) { cfg.dataStartRow = row }
}

// WithStrictHeader is kept for compatibility; it is equivalent to requiring
// every struct column to exist (HeaderPolicy.Missing = Error).
func WithStrictHeader(strict bool) ReadOption {
	return func(cfg *readConfig) {
		if strict {
			cfg.headerPolicy.Missing = ColumnPolicyError
		} else {
			cfg.headerPolicy.Missing = ColumnPolicyIgnore
		}
	}
}

func WithHeaderPolicy(policy HeaderPolicy) ReadOption {
	return func(cfg *readConfig) { cfg.headerPolicy = policy }
}

func WithRowErrorPolicy(policy RowErrorPolicy) ReadOption {
	return func(cfg *readConfig) { cfg.rowErrorPolicy = policy }
}

// WithReadColumns restricts binding to the listed struct field names, in the
// given order. Unlisted fields are ignored.
func WithReadColumns(fields ...string) ReadOption {
	return func(cfg *readConfig) {
		cfg.readColumns = append([]string(nil), fields...)
	}
}

// WithRawCellValue makes the reader use excelize's raw cell values (no number
// format applied). Enable this to read dates as serial numbers or keep original
// numeric precision.
func WithRawCellValue(raw bool) ReadOption {
	return func(cfg *readConfig) { cfg.rawCellValue = raw }
}

func WithTrimSpace(trim bool) ReadOption {
	return func(cfg *readConfig) { cfg.trimSpace = trim }
}

// WithThousandSeparator enables stripping of the given rune before numeric
// parsing (e.g. ',' for "1,234"). Disabled by default (strict parsing).
func WithThousandSeparator(sep rune) ReadOption {
	return func(cfg *readConfig) { cfg.thousandSep = sep }
}

// WithTimeLayouts sets the layouts tried when parsing time.Time fields from
// strings, before falling back to Excel serial-number conversion.
func WithTimeLayouts(layouts ...string) ReadOption {
	return func(cfg *readConfig) {
		cfg.timeLayouts = append([]string(nil), layouts...)
	}
}

// WithEmptyAsNil leaves pointer/slice/map fields nil (instead of zero) when the
// cell is empty.
func WithEmptyAsNil(on bool) ReadOption {
	return func(cfg *readConfig) { cfg.emptyAsNil = on }
}

func WithWriteColumns(columns ...string) WriteOption {
	return func(cfg *writeConfig) {
		cfg.columns = append([]string(nil), columns...)
	}
}

func WithNilRowPolicy(policy NilRowPolicy) WriteOption {
	return func(cfg *writeConfig) { cfg.nilRowPolicy = policy }
}

func WithWriteTimeLayouts(layouts ...string) WriteOption {
	return func(cfg *writeConfig) {
		cfg.timeLayouts = append([]string(nil), layouts...)
	}
}
