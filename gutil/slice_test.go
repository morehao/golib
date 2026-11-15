package gutil

import (
	"reflect"
	"testing"
)

func TestSliceDiff(t *testing.T) {
	tests := []struct {
		name string
		a, b []int
		want []int
	}{
		{"basic", []int{1, 2, 3}, []int{1, 2, 4}, []int{3}},
		{"none-left", []int{1, 2}, []int{1, 2}, nil},
		{"all-left", []int{1, 2}, []int{}, []int{1, 2}},
		{"keep-dup-in-a", []int{1, 1, 2, 3}, []int{2}, []int{1, 1, 3}},
		{"empty-a", []int{}, []int{1}, nil},
		{"both-empty", []int{}, []int{}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := SliceDiff(tc.a, tc.b)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("SliceDiff(%v,%v) = %v (nil=%v), want %v (nil=%v)",
					tc.a, tc.b, got, got == nil, tc.want, tc.want == nil)
			}
		})
	}
}

func TestSliceDuplicate(t *testing.T) {
	tests := []struct {
		name string
		in   []int
		want []int
	}{
		{"basic", []int{1, 2, 1, 3, 2}, []int{1, 2, 3}},
		{"no-dup", []int{1, 2, 3}, []int{1, 2, 3}},
		{"all-same", []int{5, 5, 5}, []int{5}},
		{"empty", []int{}, []int{}},
		{"non-adjacent-dup", []int{1, 2, 3, 1, 2, 3}, []int{1, 2, 3}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := SliceDuplicate(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("SliceDuplicate(%v) = %v, want %v", tc.in, got, tc.want)
			}
			// 语义确认：空输入返回非 nil 空切片（与原实现一致）
			if got == nil && tc.want != nil {
				t.Fatalf("SliceDuplicate(%v) returned nil, want non-nil", tc.in)
			}
		})
	}
}

func TestSliceContain(t *testing.T) {
	s := []string{"a", "b", "c"}
	if !SliceContain(s, "a") {
		t.Fatal("SliceContain should find a")
	}
	if SliceContain(s, "z") {
		t.Fatal("SliceContain should not find z")
	}
	if SliceContain([]string{}, "a") {
		t.Fatal("SliceContain on empty should be false")
	}
	if SliceContain(nil, "a") {
		t.Fatal("SliceContain on nil should be false")
	}
}

func TestSliceGroup(t *testing.T) {
	tests := []struct {
		name     string
		slice    []int
		group    int
		expected [][]int
	}{
		{
			name:  "exact division",
			slice: makeRange(1, 100),
			group: 20,
			expected: [][]int{
				makeRange(1, 20),
				makeRange(21, 40),
				makeRange(41, 60),
				makeRange(61, 80),
				makeRange(81, 100),
			},
		},
		{
			name:     "tail remainder",
			slice:    []int{1, 2, 3, 4, 5},
			group:    2,
			expected: [][]int{{1, 2}, {3, 4}, {5}},
		},
		{
			name:     "empty slice",
			slice:    []int{},
			group:    4,
			expected: [][]int{},
		},
		{
			name:     "invalid group size",
			slice:    []int{1, 2, 3},
			group:    0,
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SliceGroup(tt.slice, tt.group)
			if !reflect.DeepEqual(got, tt.expected) {
				t.Fatalf("SliceGroup() = %v, want %v", got, tt.expected)
			}
		})
	}
}

// TestSliceGroup_IndependentCopies 确认分组结果是独立副本（修改分组不影响原切片）。
func TestSliceGroup_IndependentCopies(t *testing.T) {
	src := []int{1, 2, 3, 4}
	groups := SliceGroup(src, 2)
	groups[0][0] = 99
	if src[0] != 1 {
		t.Fatalf("SliceGroup chunks alias source slice, src[0]=%d want 1", src[0])
	}
}

func makeRange(start, end int) []int {
	if end < start {
		return []int{}
	}
	result := make([]int, end-start+1)
	for i := range result {
		result[i] = start + i
	}
	return result
}
