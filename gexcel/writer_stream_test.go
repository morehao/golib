package gexcel

import (
	"path/filepath"
	"testing"
)

func TestWriteStream_RoundTrip(t *testing.T) {
	type row struct {
		Name string `excel:"col=姓名"`
		Age  int    `excel:"col=年龄"`
	}
	rows := []row{{Name: "张三", Age: 18}, {Name: "李四", Age: 22}}
	path := filepath.Join(t.TempDir(), "stream.xlsx")
	if err := WriteStream(path, rows, WithWriteSheet("Data")); err != nil {
		t.Fatalf("WriteStream: %v", err)
	}
	got, rowErrs, err := ReadAll[row](path, WithReadSheet("Data"))
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(rowErrs) != 0 {
		t.Fatal(rowErrs)
	}
	if len(got) != 2 || got[0].Name != "张三" || got[1].Age != 22 {
		t.Fatalf("unexpected: %+v", got)
	}
}

func TestWriteStream_NoResidualSheet1(t *testing.T) {
	type row struct {
		Name string `excel:"col=姓名"`
	}
	path := filepath.Join(t.TempDir(), "s.xlsx")
	if err := WriteStream(path, []row{{Name: "x"}}, WithWriteSheet("Data")); err != nil {
		t.Fatal(err)
	}
	got, _, _ := ReadAll[row](path, WithReadSheet("Data"))
	if len(got) != 1 {
		t.Fatalf("unexpected: %+v", got)
	}
}
