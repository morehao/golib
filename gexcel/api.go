package gexcel

import (
	"bytes"

	"github.com/xuri/excelize/v2"
)

// ReadFile streams the sheet at path through fn, one decoded row at a time.
// Peak memory is O(columns), so it is safe for very large sheets. It returns
// the collected RowErrors after finishing; a non-nil error aborts the read.
func ReadFile[T any](path string, fn func(T) error, opts ...ReadOption) ([]RowError, error) {
	f, err := excelize.OpenFile(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return ReadFrom[T](f, fn, opts...)
}

// ReadAll reads the whole sheet into a slice. It uses the same streaming path
// as ReadFile internally.
func ReadAll[T any](path string, opts ...ReadOption) ([]T, []RowError, error) {
	var out []T
	rowErrs, err := ReadFile[T](path, func(row T) error {
		out = append(out, row)
		return nil
	}, opts...)
	return out, rowErrs, err
}

// ReadFrom streams rows from an already-open *excelize.File (multi-sheet or
// reused workbook scenarios).
func ReadFrom[T any](f *excelize.File, fn func(T) error, opts ...ReadOption) ([]RowError, error) {
	cfg := mergeReadConfig(opts)
	return readSheet[T](f, cfg, fn)
}

// WriteFile writes rows to a new xlsx file at path.
func WriteFile[T any](path string, rows []T, opts ...WriteOption) error {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	cfg := mergeWriteConfig(opts)
	if err := writeSheet(f, rows, cfg); err != nil {
		return err
	}
	return f.SaveAs(path)
}

// WriteBytes writes rows and returns the xlsx as bytes.
func WriteBytes[T any](rows []T, opts ...WriteOption) ([]byte, error) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	cfg := mergeWriteConfig(opts)
	if err := writeSheet(f, rows, cfg); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// WriteTo appends rows into an existing workbook's sheet.
func WriteTo[T any](f *excelize.File, sheet string, rows []T, opts ...WriteOption) error {
	if f == nil {
		return ErrNilFile
	}
	cfg := mergeWriteConfig(opts)
	cfg.sheet = sheet
	return writeSheet(f, rows, cfg)
}

func mergeReadConfig(opts []ReadOption) readConfig {
	cfg := defaultReadConfig()
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		opt(&cfg)
	}
	return cfg
}

func mergeWriteConfig(opts []WriteOption) writeConfig {
	cfg := defaultWriteConfig()
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		opt(&cfg)
	}
	return cfg
}
