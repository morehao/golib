package excel

import "testing"

func TestDefaultReadConfig(t *testing.T) {
	cfg := defaultReadConfig()

	if cfg.sheet != "Sheet1" {
		t.Fatalf("expected default sheet to be Sheet1, got %q", cfg.sheet)
	}
	if cfg.headerRow != 1 {
		t.Fatalf("expected default headerRow to be 1, got %d", cfg.headerRow)
	}
	if cfg.dataStartRow != 2 {
		t.Fatalf("expected default dataStartRow to be 2, got %d", cfg.dataStartRow)
	}
}

func TestWriterInitOptionFromWriteOptions(t *testing.T) {
	opt := WriterInitOptionFromWriteOptions(
		WithWriteSheet("Report"),
		WithWriteHeaderRow(3),
	)

	if opt == nil {
		t.Fatal("expected option not nil")
	}
	if opt.SheetName != "Report" {
		t.Fatalf("expected sheet name Report, got %q", opt.SheetName)
	}
	if opt.HeadRow != 2 {
		t.Fatalf("expected 0-based head row 2, got %d", opt.HeadRow)
	}
}

func TestNewWriteWithOptions(t *testing.T) {
	w := NewWriteWithOptions(
		WithWriteSheet("Export"),
		WithWriteHeaderRow(2),
	)

	if w == nil {
		t.Fatal("expected writer not nil")
	}
	if w.sheetName != "Export" {
		t.Fatalf("expected sheet name Export, got %q", w.sheetName)
	}
	if w.headRow != 1 {
		t.Fatalf("expected 0-based head row 1, got %d", w.headRow)
	}
}

func TestWriterInitOptionFromWriteOptionsDoesNotSilentlyFixInvalidHeaderRow(t *testing.T) {
	opt := WriterInitOptionFromWriteOptions(WithWriteHeaderRow(0))

	if opt == nil {
		t.Fatal("expected option not nil")
	}
	if opt.HeadRow != -1 {
		t.Fatalf("expected invalid header row to remain invalid after 1-based to 0-based conversion, got %d", opt.HeadRow)
	}
}
