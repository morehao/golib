package excel

import (
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestReadRows_CollectRowErrors(t *testing.T) {
	type row struct {
		Name string `excel:"col=姓名,alias=名字"`
		Age  int    `excel:"col=年龄"`
	}

	f := excelize.NewFile()
	sheet := f.GetSheetName(0)

	if err := f.SetSheetRow(sheet, "A1", &[]interface{}{"姓名", "名字", "年龄"}); err != nil {
		t.Fatalf("set header row failed: %v", err)
	}
	if err := f.SetSheetRow(sheet, "A2", &[]interface{}{"张三", "备用名", "20"}); err != nil {
		t.Fatalf("set first data row failed: %v", err)
	}
	if err := f.SetSheetRow(sheet, "A3", &[]interface{}{"李四", "备用名2", "abc"}); err != nil {
		t.Fatalf("set second data row failed: %v", err)
	}

	cfg := defaultReadConfig()
	cfg.sheet = sheet
	cfg.headerRow = 1
	cfg.dataStartRow = 2

	rows, rowErrors, err := readRows[row](f, cfg)
	if err != nil {
		t.Fatalf("readRows returned error: %v", err)
	}

	if len(rows) != 1 {
		t.Fatalf("expected 1 valid row, got %d", len(rows))
	}
	if rows[0].Name != "张三" {
		t.Fatalf("expected valid row name 张三, got %q", rows[0].Name)
	}
	if rows[0].Age != 20 {
		t.Fatalf("expected valid row age 20, got %d", rows[0].Age)
	}

	if len(rowErrors) != 1 {
		t.Fatalf("expected 1 row error, got %d", len(rowErrors))
	}
	if rowErrors[0].Code != RowErrorCodeTypeMismatch {
		t.Fatalf("expected row error code %s, got %s", RowErrorCodeTypeMismatch, rowErrors[0].Code)
	}
	if rowErrors[0].Row != 3 {
		t.Fatalf("expected row error row 3, got %d", rowErrors[0].Row)
	}
	if rowErrors[0].Column != "年龄" {
		t.Fatalf("expected row error column 年龄, got %q", rowErrors[0].Column)
	}
	if rowErrors[0].Value != "abc" {
		t.Fatalf("expected row error value abc, got %q", rowErrors[0].Value)
	}
}
