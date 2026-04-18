package testkit

import (
	"os"
	"path/filepath"
	"runtime"
)

func OpenFile(relPath string) (*os.File, error) {
	_, filename, _, ok := runtime.Caller(1)
	if !ok {
		panic("cannot get caller info")
	}
	fullPath := filepath.Join(filepath.Dir(filename), relPath)
	return os.Open(fullPath)
}
