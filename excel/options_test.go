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
