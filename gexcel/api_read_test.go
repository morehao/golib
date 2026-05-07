package gexcel

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

type readAPIRow struct {
	Name string `excel:"col=姓名"`
	Age  int    `excel:"col=年龄"`
}

func TestReadFromExcelize(t *testing.T) {
	f := excelize.NewFile()
	sheet := f.GetSheetName(0)

	if err := f.SetSheetRow(sheet, "A1", &[]interface{}{"姓名", "年龄"}); err != nil {
		t.Fatalf("set header row failed: %v", err)
	}
	if err := f.SetSheetRow(sheet, "A2", &[]interface{}{"张三", "20"}); err != nil {
		t.Fatalf("set data row failed: %v", err)
	}

	rows, rowErrs, err := ReadFromExcelize[readAPIRow](f, WithReadSheet(sheet))
	if err != nil {
		t.Fatalf("ReadFromExcelize returned error: %v", err)
	}
	if len(rowErrs) != 0 {
		t.Fatalf("expected 0 row errors, got %d", len(rowErrs))
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0].Name != "张三" {
		t.Fatalf("expected name 张三, got %q", rows[0].Name)
	}
	if rows[0].Age != 20 {
		t.Fatalf("expected age 20, got %d", rows[0].Age)
	}
}

func TestReadFile(t *testing.T) {
	f := excelize.NewFile()
	sheet := f.GetSheetName(0)

	if err := f.SetSheetRow(sheet, "A1", &[]interface{}{"姓名", "年龄"}); err != nil {
		t.Fatalf("set header row failed: %v", err)
	}
	if err := f.SetSheetRow(sheet, "A2", &[]interface{}{"李四", "30"}); err != nil {
		t.Fatalf("set data row failed: %v", err)
	}

	path := filepath.Join(t.TempDir(), "read_api.xlsx")
	if err := f.SaveAs(path); err != nil {
		t.Fatalf("save excel file failed: %v", err)
	}

	rows, rowErrs, err := ReadFile[readAPIRow](path, WithReadSheet(sheet))
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if len(rowErrs) != 0 {
		t.Fatalf("expected 0 row errors, got %d", len(rowErrs))
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0].Name != "李四" {
		t.Fatalf("expected name 李四, got %q", rows[0].Name)
	}
	if rows[0].Age != 30 {
		t.Fatalf("expected age 30, got %d", rows[0].Age)
	}
}

func TestReadFromExcelize_StrictHeader(t *testing.T) {
	f := excelize.NewFile()
	sheet := f.GetSheetName(0)

	if err := f.SetSheetRow(sheet, "A1", &[]interface{}{"姓名"}); err != nil {
		t.Fatalf("set header row failed: %v", err)
	}
	if err := f.SetSheetRow(sheet, "A2", &[]interface{}{"张三"}); err != nil {
		t.Fatalf("set data row failed: %v", err)
	}

	rows, rowErrs, err := ReadFromExcelize[readAPIRow](f, WithReadSheet(sheet), WithStrictHeader(true))
	if err == nil {
		t.Fatalf("expected strict header error, got nil")
	}
	if !strings.Contains(err.Error(), "required column \"年龄\" not found") {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("expected 0 rows, got %d", len(rows))
	}
	if len(rowErrs) != 0 {
		t.Fatalf("expected 0 row errors, got %d", len(rowErrs))
	}
}
