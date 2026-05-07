package gexcel

import "testing"

func TestRowErrorCodeRequiredMissCompatibilityAlias(t *testing.T) {
	if RowErrorCodeRequiredMiss != RowErrorCodeRequiredMissing {
		t.Fatalf("expected compatibility alias to equal new constant")
	}
}
