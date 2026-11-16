package gexcel

import (
	"path/filepath"
	"strconv"
	"testing"
)

type benchRow struct {
	Name string `excel:"col=姓名"`
	Age  int    `excel:"col=年龄"`
	City string `excel:"col=城市"`
}

func benchRows(n int) []benchRow {
	out := make([]benchRow, n)
	for i := range out {
		out[i] = benchRow{Name: "user" + strconv.Itoa(i), Age: i % 90, City: "city"}
	}
	return out
}

func BenchmarkWriteFile_1k(b *testing.B) {
	rows := benchRows(1000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		path := filepath.Join(b.TempDir(), "b.xlsx")
		if err := WriteFile(path, rows); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkWriteStream_1k(b *testing.B) {
	rows := benchRows(1000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		path := filepath.Join(b.TempDir(), "b.xlsx")
		if err := WriteStream(path, rows); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReadAll_1k(b *testing.B) {
	rows := benchRows(1000)
	path := filepath.Join(b.TempDir(), "r.xlsx")
	if err := WriteFile(path, rows); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := ReadAll[benchRow](path); err != nil {
			b.Fatal(err)
		}
	}
}
