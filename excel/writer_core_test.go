package excel

import (
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestWriteWorkbook_HeaderAndRows(t *testing.T) {
	type row struct {
		Name string `excel:"col=姓名"`
		Age  int    `excel:"col=年龄"`
	}

	f := excelize.NewFile()
	sheet := f.GetSheetName(0)

	cfg := defaultWriteConfig()
	cfg.sheet = sheet
	cfg.headerRow = 2

	rows := []row{{Name: "张三", Age: 18}}
	if err := writeWorkbook(f, rows, cfg); err != nil {
		t.Fatalf("writeWorkbook returned error: %v", err)
	}

	headers, err := f.GetRows(sheet)
	if err != nil {
		t.Fatalf("GetRows returned error: %v", err)
	}
	if len(headers) < 3 {
		t.Fatalf("expected at least 3 rows, got %d", len(headers))
	}

	if headers[1][0] != "姓名" || headers[1][1] != "年龄" {
		t.Fatalf("expected header row [姓名 年龄], got %#v", headers[1])
	}
	if headers[2][0] != "张三" || headers[2][1] != "18" {
		t.Fatalf("expected first data row [张三 18], got %#v", headers[2])
	}
}
