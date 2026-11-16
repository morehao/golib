package gexcel

import (
	"errors"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"
)

func TestReadAll_RawCellValueDateSerial(t *testing.T) {
	type row struct {
		At time.Time `excel:"col=日期"`
	}
	f := excelize.NewFile()
	sheet := f.GetSheetName(0)
	f.SetSheetRow(sheet, "A1", &[]interface{}{"日期"})
	// Write a numeric serial (45567 == 2024-10-01 in 1900 system, roughly).
	f.SetCellInt(sheet, "A2", 45567)

	path := writeTemp(t, f)
	got, rowErrs, err := ReadAll[row](path, WithReadSheet(sheet), WithRawCellValue(true))
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(rowErrs) != 0 {
		t.Fatalf("row errors: %+v", rowErrs)
	}
	if len(got) != 1 || got[0].At.IsZero() {
		t.Fatalf("expected a parsed time, got %+v", got)
	}
	// 45567 in 1900 system = 1899-12-30 + 45567 days.
	want := excelEpoch(false).Add(time.Duration(45567) * 24 * time.Hour)
	if !got[0].At.Equal(want) {
		t.Fatalf("got %v want %v", got[0].At, want)
	}
}

func TestReadAll_ReadColumnsOrder(t *testing.T) {
	type row struct {
		Name string `excel:"col=姓名"`
		Age  int    `excel:"col=年龄"`
	}
	f, sheet := newSheetFile(t, []any{"姓名", "年龄"}, []any{"张三", "20"})
	// Restrict to Age only: Name should stay zero-value.
	got, _, err := ReadAll[row](writeTemp(t, f), WithReadSheet(sheet), WithReadColumns("Age"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Age != 20 || got[0].Name != "" {
		t.Fatalf("expected only Age bound, got %+v", got)
	}
}

func TestHeaderPolicy_MissingWarn(t *testing.T) {
	type row struct {
		Name string `excel:"col=姓名"`
		Age  int    `excel:"col=年龄"`
	}
	f, sheet := newSheetFile(t, []any{"姓名"}, []any{"张三"})
	var n int
	rowErrs, err := ReadFrom[row](f, func(row) error { n++; return nil },
		WithReadSheet(sheet),
		WithHeaderPolicy(HeaderPolicy{Missing: ColumnPolicyWarn}))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 row, got %d", n)
	}
	if len(rowErrs) != 1 || rowErrs[0].Severity != SeverityWarning || rowErrs[0].Code != RowErrorCodeRequiredMissing {
		t.Fatalf("expected 1 warning RowError, got %+v", rowErrs)
	}
}

func TestSentinels(t *testing.T) {
	if _, _, err := ReadAll[struct {
		A string `excel:"col=A"`
	}]("/nonexistent/file.xlsx"); err == nil {
		t.Fatalf("expected open error")
	}
	// nil file
	if _, err := ReadFrom[struct {
		A string `excel:"col=A"`
	}](nil, nil); !errors.Is(err, ErrNilFile) {
		t.Fatalf("expected ErrNilFile, got %v", err)
	}
	// no matched columns
	f, sheet := newSheetFile(t, []any{"无关列"})
	_, err := ReadFrom[struct {
		A string `excel:"col=A"`
	}](f, nil, WithReadSheet(sheet))
	if !errors.Is(err, ErrNoMatchedColumns) {
		t.Fatalf("expected ErrNoMatchedColumns, got %v", err)
	}
}
