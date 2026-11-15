package gutil

import "testing"

// TestCopyMap_Equivalence 对照 maps.Clone：非 nil map 结果应与原 map 内容一致，
// 且为独立副本（修改副本不影响原 map）。
func TestCopyMap_Equivalence(t *testing.T) {
	tests := []struct {
		name string
		in   map[string]int
	}{
		{"normal", map[string]int{"a": 1, "b": 2}},
		{"single", map[string]int{"x": 42}},
		{"empty-nonnil", map[string]int{}},
		{"nil", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := CopyMap(tc.in)
			if got == nil {
				t.Fatalf("CopyMap(%v) returned nil, want non-nil map", tc.in)
			}
			if len(got) != len(tc.in) {
				t.Fatalf("CopyMap len = %d, want %d", len(got), len(tc.in))
			}
			for k, v := range tc.in {
				if got[k] != v {
					t.Fatalf("CopyMap[%q] = %d, want %d", k, got[k], v)
				}
			}
			// 独立性：修改副本不应影响原 map
			if tc.in != nil {
				got["__probe__"] = -1
				if _, mutated := tc.in["__probe__"]; mutated {
					t.Fatalf("CopyMap returned aliased map, original was mutated")
				}
			}
		})
	}
}

func TestCopyMap_GenericTypes(t *testing.T) {
	m := map[int]string{1: "a", 2: "b"}
	got := CopyMap(m)
	if len(got) != 2 || got[1] != "a" || got[2] != "b" {
		t.Fatalf("CopyMap generic mismatch: %v", got)
	}
}
