package excel

import "github.com/xuri/excelize/v2"

func ReadFile[T any](path string, opts ...ReadOption) ([]T, []RowError, error) {
	f, err := excelize.OpenFile(path)
	if err != nil {
		return nil, nil, err
	}
	defer func() {
		_ = f.Close()
	}()

	return ReadFromExcelize[T](f, opts...)
}

func ReadFromExcelize[T any](f *excelize.File, opts ...ReadOption) ([]T, []RowError, error) {
	cfg := defaultReadConfig()
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		opt(&cfg)
	}

	return readRows[T](f, cfg)
}
