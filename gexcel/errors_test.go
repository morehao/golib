package gexcel

import "testing"

func TestRowErrorCodesDistinct(t *testing.T) {
	codes := []string{
		RowErrorCodeTypeMismatch,
		RowErrorCodeRequiredMissing,
		RowErrorCodeColumnNotFound,
		RowErrorCodeDuplicateColumn,
		RowErrorCodeUnknownColumn,
	}
	seen := make(map[string]struct{}, len(codes))
	for _, c := range codes {
		if c == "" {
			t.Fatalf("row error code must not be empty")
		}
		if _, dup := seen[c]; dup {
			t.Fatalf("duplicate row error code %q", c)
		}
		seen[c] = struct{}{}
	}
}
