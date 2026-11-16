package gexcel

import (
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestWriteSheet_HeaderAndRows(t *testing.T) {
	type row struct {
		Name string `excel:"col=姓名"`
		Age  int    `excel:"col=年龄"`
	}
	f := excelize.NewFile()
	sheet := f.GetSheetName(0)
	cfg := defaultWriteConfig()
	cfg.sheet = sheet
	cfg.headerRow = 2

	if err := writeSheet(f, []row{{Name: "张三", Age: 18}}, cfg); err != nil {
		t.Fatalf("writeSheet: %v", err)
	}
	grid, err := f.GetRows(sheet)
	if err != nil {
		t.Fatal(err)
	}
	if len(grid) < 3 {
		t.Fatalf("expected >=3 rows, got %d", len(grid))
	}
	if grid[1][0] != "姓名" || grid[1][1] != "年龄" {
		t.Fatalf("header row: %v", grid[1])
	}
	if grid[2][0] != "张三" || grid[2][1] != "18" {
		t.Fatalf("data row: %v", grid[2])
	}
}

func TestWriteSheet_ExplicitColumnOrder(t *testing.T) {
	type row struct {
		Name string `excel:"col=姓名"`
		Age  int    `excel:"col=年龄"`
	}
	f := excelize.NewFile()
	sheet := f.GetSheetName(0)
	cfg := defaultWriteConfig()
	cfg.sheet = sheet
	cfg.columns = []string{"Age", "Name"}
	if err := writeSheet(f, []row{{Name: "张三", Age: 18}}, cfg); err != nil {
		t.Fatal(err)
	}
	grid, _ := f.GetRows(sheet)
	if grid[0][0] != "年龄" || grid[0][1] != "姓名" {
		t.Fatalf("header order: %v", grid[0])
	}
	if grid[1][0] != "18" || grid[1][1] != "张三" {
		t.Fatalf("data order: %v", grid[1])
	}
}

func TestOrderColumns(t *testing.T) {
	base := []columnSchema{{fieldName: "A", column: "a"}, {fieldName: "B", column: "b"}, {fieldName: "C", column: "c"}}
	got := orderColumns(base, []string{"C", "A"})
	if len(got) != 3 || got[0].fieldName != "C" || got[1].fieldName != "A" || got[2].fieldName != "B" {
		t.Fatalf("unexpected order: %+v", got)
	}
}
