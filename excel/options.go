package excel

type UnknownColumnPolicy string

const (
	UnknownColumnPolicyIgnore UnknownColumnPolicy = "ignore"
	UnknownColumnPolicyError  UnknownColumnPolicy = "error"
)

type readConfig struct {
	sheet               string
	headerRow           int
	dataStartRow        int
	strictHeader        bool
	unknownColumnPolicy UnknownColumnPolicy
	requiredColumns     []string
	columns             []string
}

type writeConfig struct {
	sheet     string
	headerRow int
	columns   []string
}

type ReadOption func(*readConfig)

type WriteOption func(*writeConfig)

func defaultReadConfig() readConfig {
	return readConfig{
		sheet:               "Sheet1",
		headerRow:           1,
		dataStartRow:        2,
		unknownColumnPolicy: UnknownColumnPolicyIgnore,
	}
}

func defaultWriteConfig() writeConfig {
	return writeConfig{
		sheet:     "Sheet1",
		headerRow: 1,
	}
}

func WithReadSheet(sheet string) ReadOption {
	return func(cfg *readConfig) {
		cfg.sheet = sheet
	}
}

func WithWriteSheet(sheet string) WriteOption {
	return func(cfg *writeConfig) {
		cfg.sheet = sheet
	}
}

func WithHeaderRow(row int) ReadOption {
	return func(cfg *readConfig) {
		cfg.headerRow = row
	}
}

func WithWriteHeaderRow(row int) WriteOption {
	return func(cfg *writeConfig) {
		cfg.headerRow = row
	}
}

func WithDataStartRow(row int) ReadOption {
	return func(cfg *readConfig) {
		cfg.dataStartRow = row
	}
}

func WithStrictHeader(strict bool) ReadOption {
	return func(cfg *readConfig) {
		cfg.strictHeader = strict
	}
}

func WithUnknownColumnPolicy(policy UnknownColumnPolicy) ReadOption {
	return func(cfg *readConfig) {
		cfg.unknownColumnPolicy = policy
	}
}

func WithRequiredColumns(columns ...string) ReadOption {
	return func(cfg *readConfig) {
		cfg.requiredColumns = append([]string(nil), columns...)
	}
}

func WithReadColumns(columns ...string) ReadOption {
	return func(cfg *readConfig) {
		cfg.columns = append([]string(nil), columns...)
	}
}

func WithWriteColumns(columns ...string) WriteOption {
	return func(cfg *writeConfig) {
		cfg.columns = append([]string(nil), columns...)
	}
}
