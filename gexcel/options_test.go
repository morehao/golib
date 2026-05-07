package gexcel

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

func TestDefaultWriteConfig(t *testing.T) {
	cfg := defaultWriteConfig()

	if cfg.sheet != "Sheet1" {
		t.Fatalf("expected default sheet to be Sheet1, got %q", cfg.sheet)
	}
	if cfg.headerRow != 1 {
		t.Fatalf("expected default headerRow to be 1, got %d", cfg.headerRow)
	}
}

func TestWriteOptions(t *testing.T) {
	cfg := defaultWriteConfig()

	WithWriteSheet("Report")(&cfg)
	WithWriteHeaderRow(3)(&cfg)
	WithWriteColumns("姓名", "年龄")(&cfg)

	if cfg.sheet != "Report" {
		t.Fatalf("expected sheet name Report, got %q", cfg.sheet)
	}
	if cfg.headerRow != 3 {
		t.Fatalf("expected headerRow 3, got %d", cfg.headerRow)
	}
	if len(cfg.columns) != 2 || cfg.columns[0] != "姓名" || cfg.columns[1] != "年龄" {
		t.Fatalf("unexpected write columns: %#v", cfg.columns)
	}
}
