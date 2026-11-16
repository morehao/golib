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

func newSheetFile(t *testing.T, header []any, data ...[]any) (*excelize.File, string) {
	t.Helper()
	f := excelize.NewFile()
	sheet := f.GetSheetName(0)
	if err := f.SetSheetRow(sheet, "A1", &header); err != nil {
		t.Fatalf("set header row: %v", err)
	}
	for i, row := range data {
		cell, _ := excelize.CoordinatesToCellName(1, i+2)
		if err := f.SetSheetRow(sheet, cell, &row); err != nil {
			t.Fatalf("set data row %d: %v", i, err)
		}
	}
	return f, sheet
}

func TestReadAll_File(t *testing.T) {
	f, _ := newSheetFile(t, []any{"姓名", "年龄"}, []any{"李四", "30"})
	path := filepath.Join(t.TempDir(), "read_api.xlsx")
	if err := f.SaveAs(path); err != nil {
		t.Fatal(err)
	}
	rows, rowErrs, err := ReadAll[readAPIRow](path)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(rowErrs) != 0 {
		t.Fatalf("expected 0 row errors, got %d", len(rowErrs))
	}
	if len(rows) != 1 || rows[0].Name != "李四" || rows[0].Age != 30 {
		t.Fatalf("unexpected rows: %+v", rows)
	}
}

func TestReadFrom_Stream(t *testing.T) {
	f, sheet := newSheetFile(t, []any{"姓名", "年龄"}, []any{"张三", "20"}, []any{"王五", "25"})
	var names []string
	rowErrs, err := ReadFrom[readAPIRow](f, func(r readAPIRow) error {
		names = append(names, r.Name)
		return nil
	}, WithReadSheet(sheet))
	if err != nil {
		t.Fatalf("ReadFrom: %v", err)
	}
	if len(rowErrs) != 0 {
		t.Fatalf("row errors: %v", rowErrs)
	}
	if len(names) != 2 || names[0] != "张三" || names[1] != "王五" {
		t.Fatalf("unexpected names: %v", names)
	}
}

func TestReadFrom_MissingColumnStrict(t *testing.T) {
	f, sheet := newSheetFile(t, []any{"姓名"}, []any{"张三"})
	_, err := ReadFrom[readAPIRow](f, nil, WithReadSheet(sheet), WithStrictHeader(true))
	if err == nil || !strings.Contains(err.Error(), "required column \"年龄\" not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReadFrom_DuplicateHeaderError(t *testing.T) {
	f, sheet := newSheetFile(t, []any{"姓名", "姓名", "年龄"}, []any{"先", "后", "18"})
	_, err := ReadFrom[readAPIRow](f, nil, WithReadSheet(sheet),
		WithHeaderPolicy(HeaderPolicy{Duplicate: ColumnPolicyError}))
	if err == nil {
		t.Fatalf("expected duplicate header error")
	}
}

func TestReadFrom_DuplicateHeaderIgnoreLastWins(t *testing.T) {
	f, sheet := newSheetFile(t, []any{"姓名", "姓名", "年龄"}, []any{"先", "后", "18"})
	rows, _, err := ReadAll[readAPIRow](writeTemp(t, f), WithReadSheet(sheet))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(rows) != 1 || rows[0].Name != "后" {
		t.Fatalf("expected last-wins 后, got %+v", rows)
	}
}

func TestReadFrom_ExtraColumnPolicy(t *testing.T) {
	f, sheet := newSheetFile(t, []any{"姓名", "年龄", "备注"}, []any{"张三", "18", "x"})
	_, err := ReadFrom[readAPIRow](f, nil, WithReadSheet(sheet),
		WithHeaderPolicy(HeaderPolicy{Extra: ColumnPolicyError}))
	if err == nil || !strings.Contains(err.Error(), "unexpected column") {
		t.Fatalf("expected extra column error, got %v", err)
	}
}

func TestReadFrom_FailFast(t *testing.T) {
	f, sheet := newSheetFile(t, []any{"姓名", "年龄"}, []any{"张三", "abc"})
	_, err := ReadFrom[readAPIRow](f, nil, WithReadSheet(sheet), WithRowErrorPolicy(RowErrorFailFast))
	if err == nil {
		t.Fatalf("expected fail-fast error")
	}
}

func TestReadFrom_Collect(t *testing.T) {
	f, sheet := newSheetFile(t, []any{"姓名", "年龄"}, []any{"张三", "20"}, []any{"李四", "abc"})
	var n int
	rowErrs, err := ReadFrom[readAPIRow](f, func(readAPIRow) error { n++; return nil }, WithReadSheet(sheet))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if n != 1 || len(rowErrs) != 1 || rowErrs[0].Row != 3 {
		t.Fatalf("got n=%d errs=%+v", n, rowErrs)
	}
}

func writeTemp(t *testing.T, f *excelize.File) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "tmp.xlsx")
	if err := f.SaveAs(p); err != nil {
		t.Fatal(err)
	}
	return p
}
