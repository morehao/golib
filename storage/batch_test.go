package storage

import (
	"errors"
	"testing"
)

func TestDeleteObjectsChunked(t *testing.T) {
	keys := func(n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = string(rune('a' + i%26))
		}
		return out
	}

	cases := []struct {
		name     string
		n        int
		maxBatch int
		wantLen  []int
	}{
		{"empty is no-op", 0, 1000, nil},
		{"below limit single batch", 3, 1000, []int{3}},
		{"exactly at limit", 4, 4, []int{4}},
		{"one over limit splits", 5, 4, []int{4, 1}},
		{"exactly two batches", 8, 4, []int{4, 4}},
		{"uneven tail", 10, 4, []int{4, 4, 2}},
		{"unlimited backend single batch", 10, 0, []int{10}},
		{"negative limit treated as unlimited", 10, -1, []int{10}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var got [][]string
			err := DeleteObjectsChunked(keys(c.n), c.maxBatch, func(batch []string) error {
				got = append(got, append([]string(nil), batch...))
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(c.wantLen) {
				t.Fatalf("批数 = %d, want %d", len(got), len(c.wantLen))
			}
			total := 0
			for i, b := range got {
				if len(b) != c.wantLen[i] {
					t.Errorf("第 %d 批大小 = %d, want %d", i, len(b), c.wantLen[i])
				}
				total += len(b)
			}
			if total != c.n {
				t.Errorf("切分后总数 = %d, want %d（不能丢 key）", total, c.n)
			}
		})
	}
}

// 空列表不得调用 del —— 否则会给后端发一次无意义的请求。
func TestDeleteObjectsChunked_EmptyNeverCallsDelete(t *testing.T) {
	called := false
	err := DeleteObjectsChunked(nil, 1000, func([]string) error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if called {
		t.Error("空列表不应调用 del")
	}
}

// 中途失败必须立刻返回，不再继续后续批次。
func TestDeleteObjectsChunked_StopsOnError(t *testing.T) {
	sentinel := errors.New("boom")
	calls := 0
	err := DeleteObjectsChunked([]string{"a", "b", "c", "d"}, 2, func([]string) error {
		calls++
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want sentinel", err)
	}
	if calls != 1 {
		t.Errorf("首次失败后应停止, 实际调用 %d 次", calls)
	}
}
