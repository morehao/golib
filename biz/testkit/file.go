package testkit

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

func OpenFile(relPath string) (*os.File, error) {
	_, filename, _, ok := runtime.Caller(1)
	if !ok {
		panic("cannot get caller info")
	}
	fullPath := filepath.Join(filepath.Dir(filename), relPath)
	return os.Open(fullPath)
}

func generateRequestID() string {
	timestamp := time.Now().UnixNano()
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("test-%d", timestamp)
	}
	return fmt.Sprintf("test-%d-%s", timestamp, hex.EncodeToString(b))
}
