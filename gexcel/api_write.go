package gexcel

import (
	"bytes"

	"github.com/xuri/excelize/v2"
)

func WriteFile[T any](rows []T, path string, opts ...WriteOption) error {
	f := excelize.NewFile()
	defer func() {
		_ = f.Close()
	}()

	cfg := defaultWriteConfig()
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		opt(&cfg)
	}

	if err := writeWorkbook(f, rows, cfg); err != nil {
		return err
	}

	return f.SaveAs(path)
}

func WriteBytes[T any](rows []T, opts ...WriteOption) ([]byte, error) {
	f := excelize.NewFile()
	defer func() {
		_ = f.Close()
	}()

	cfg := defaultWriteConfig()
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		opt(&cfg)
	}

	if err := writeWorkbook(f, rows, cfg); err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}
