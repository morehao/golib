package gutils

import (
	"reflect"
	"testing"
)

func TestSliceDiff(t *testing.T) {
	s1 := []int{1, 2, 3}
	s2 := []int{1, 2, 4}
	diff := SliceDiff(s1, s2)
	t.Log(ToJsonString(diff))
}

func TestSliceContain(t *testing.T) {
	s := []string{"a", "b"}
	t.Log(SliceContain(s, "a"))
}

func TestSliceDuplicate(t *testing.T) {
	s := []string{"a", "b", "a"}
	t.Log(SliceDuplicate(s))
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
			name:  "tail remainder",
			slice: []int{1, 2, 3, 4, 5},
			group: 2,
			expected: [][]int{{1, 2}, {3, 4}, {5}},
		},
		{
			name:     "empty slice",
			slice:   []int{},
			group:   4,
			expected: [][]int{},
		},
		{
			name:     "invalid group size",
			slice:   []int{1, 2, 3},
			group:   0,
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
