package gexcel

import (
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestReadSheet_AliasAndRowError(t *testing.T) {
	type row struct {
		Name string `excel:"col=姓名,alias=名字"`
		Age  int    `excel:"col=年龄"`
	}
	f := excelize.NewFile()
	sheet := f.GetSheetName(0)
	f.SetSheetRow(sheet, "A1", &[]interface{}{"姓名", "年龄"})
	f.SetSheetRow(sheet, "A2", &[]interface{}{"张三", "20"})
	f.SetSheetRow(sheet, "A3", &[]interface{}{"李四", "abc"})

	var got []row
	cfg := defaultReadConfig()
	cfg.sheet = sheet
	rowErrs, err := readSheet[row](f, cfg, func(r row) error { got = append(got, r); return nil })
	if err != nil {
		t.Fatalf("readSheet: %v", err)
	}
	if len(got) != 1 || got[0].Name != "张三" || got[0].Age != 20 {
		t.Fatalf("unexpected rows: %+v", got)
	}
	if len(rowErrs) != 1 || rowErrs[0].Row != 3 || rowErrs[0].Column != "年龄" || rowErrs[0].Field != "Age" {
		t.Fatalf("unexpected row errors: %+v", rowErrs)
	}
}

func TestReadSheet_NoMatchedColumns(t *testing.T) {
	type row struct {
		Name string `excel:"col=姓名"`
	}
	f := excelize.NewFile()
	sheet := f.GetSheetName(0)
	f.SetSheetRow(sheet, "A1", &[]interface{}{"城市"})
	f.SetSheetRow(sheet, "A2", &[]interface{}{"上海"})
	cfg := defaultReadConfig()
	cfg.sheet = sheet
	_, err := readSheet[row](f, cfg, nil)
	if err == nil {
		t.Fatalf("expected ErrNoMatchedColumns")
	}
}
