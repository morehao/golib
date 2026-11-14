package gtest

import (
	"bytes"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"testing"
)

// Update 是标准的 -update flag：go test -update 时回写金样文件。
var Update = flag.Bool("update", false, "update golden files")

// Golden 将 got 与 testdata/golden/<name> 比较；-update 时回写并跳过比较。
func Golden(t testing.TB, got []byte, name string) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name)
	if *Update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("gtest: golden mkdir %q: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("gtest: golden write %q: %v", path, err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			t.Fatalf("gtest: golden file %q missing; run `go test -update` to create it", path)
		}
		t.Fatalf("gtest: golden read %q: %v", path, err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("gtest: golden mismatch for %q\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
}
