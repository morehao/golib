package gexcel

import (
	"path/filepath"
	"testing"

	"github.com/xuri/excelize/v2"
)

type writeAPIRow struct {
	Name string `excel:"col=姓名"`
	Age  int    `excel:"col=年龄"`
}

func TestWriteFile_RoundTrip(t *testing.T) {
	rows := []writeAPIRow{{Name: "张三", Age: 20}, {Name: "李四", Age: 30}}
	path := filepath.Join(t.TempDir(), "write_api.xlsx")
	if err := WriteFile(path, rows, WithWriteSheet("Data")); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, rowErrs, err := ReadAll[writeAPIRow](path, WithReadSheet("Data"))
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(rowErrs) != 0 {
		t.Fatalf("row errors: %v", rowErrs)
	}
	if len(got) != len(rows) || got[0].Name != "张三" || got[1].Age != 30 {
		t.Fatalf("roundtrip mismatch: %+v", got)
	}
}

func TestWriteBytes(t *testing.T) {
	b, err := WriteBytes([]writeAPIRow{{Name: "王五", Age: 18}}, WithWriteSheet("Data"))
	if err != nil {
		t.Fatalf("WriteBytes: %v", err)
	}
	if len(b) == 0 {
		t.Fatal("expected non-empty bytes")
	}
}

func TestWriteFile_NoResidualSheet1(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sheets.xlsx")
	if err := WriteFile(path, []writeAPIRow{{Name: "张三", Age: 1}}, WithWriteSheet("Data")); err != nil {
		t.Fatal(err)
	}
	f, err := excelize.OpenFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	list := f.GetSheetList()
	if len(list) != 1 || list[0] != "Data" {
		t.Fatalf("expected only [Data], got %v", list)
	}
}

func TestWriteFile_NilRowError(t *testing.T) {
	type row struct {
		Name string `excel:"col=姓名"`
	}
	err := WriteFile(filepath.Join(t.TempDir(), "x.xlsx"), []*row{nil})
	if err == nil {
		t.Fatalf("expected nil-row error")
	}
}

func TestWriteFile_NilRowSkip(t *testing.T) {
	type row struct {
		Name string `excel:"col=姓名"`
	}
	b, err := WriteBytes([]*row{nil, {Name: "张三"}}, WithNilRowPolicy(NilRowSkip))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(b) == 0 {
		t.Fatal("empty bytes")
	}
}
