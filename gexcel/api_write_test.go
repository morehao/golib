package gexcel

import (
	"path/filepath"
	"testing"
)

type writeAPIRow struct {
	Name string `excel:"col=姓名"`
	Age  int    `excel:"col=年龄"`
}

func TestWriteFile(t *testing.T) {
	rows := []writeAPIRow{{Name: "张三", Age: 20}, {Name: "李四", Age: 30}}
	path := filepath.Join(t.TempDir(), "write_api.xlsx")

	if err := WriteFile(rows, path, WithWriteSheet("Data")); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	got, rowErrs, err := ReadFile[writeAPIRow](path, WithReadSheet("Data"))
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if len(rowErrs) != 0 {
		t.Fatalf("expected 0 row errors, got %d", len(rowErrs))
	}
	if len(got) != len(rows) {
		t.Fatalf("expected %d rows, got %d", len(rows), len(got))
	}
	if got[0].Name != rows[0].Name || got[0].Age != rows[0].Age {
		t.Fatalf("first row mismatch: got %+v, want %+v", got[0], rows[0])
	}
	if got[1].Name != rows[1].Name || got[1].Age != rows[1].Age {
		t.Fatalf("second row mismatch: got %+v, want %+v", got[1], rows[1])
	}
}

func TestWriteBytes(t *testing.T) {
	rows := []writeAPIRow{{Name: "王五", Age: 18}}

	b, err := WriteBytes(rows, WithWriteSheet("Data"))
	if err != nil {
		t.Fatalf("WriteBytes returned error: %v", err)
	}
	if len(b) == 0 {
		t.Fatal("expected non-empty bytes")
	}
}
