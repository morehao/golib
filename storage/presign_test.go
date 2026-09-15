package storage

import (
	"errors"
	"testing"
	"time"
)

func TestResolvePresignTTL(t *testing.T) {
	cases := []struct {
		name    string
		in      time.Duration
		want    time.Duration
		wantErr bool
	}{
		{"zero uses default", 0, PresignTTLDefault, false},
		{"explicit", time.Hour, time.Hour, false},
		{"exactly max", PresignTTLMax, PresignTTLMax, false},
		{"negative rejected", -time.Second, 0, true},
		{"over max rejected before signing", PresignTTLMax + time.Second, 0, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ResolvePresignTTL(c.in)
			if c.wantErr {
				if err == nil {
					t.Fatalf("期望报错, got %v", got)
				}
				if !errors.Is(err, ErrInvalidArgument) {
					t.Errorf("错误必须包装 ErrInvalidArgument, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("意外错误: %v", err)
			}
			if got != c.want {
				t.Errorf("ttl = %s, want %s", got, c.want)
			}
		})
	}
}

// 超限必须在**签发前**报错，而不是等到客户端使用 URL 时才 403。
func TestResolvePresignTTL_RejectsBeforeSigning(t *testing.T) {
	if _, err := ResolvePresignTTL(8 * 24 * time.Hour); err == nil {
		t.Fatal("超过 7 天必须在签发前被拒绝")
	}
}

func TestRangeInfoLen(t *testing.T) {
	cases := []struct {
		r    RangeInfo
		want int64
	}{
		{RangeInfo{0, 0}, 1},
		{RangeInfo{0, 9}, 10},
		{RangeInfo{5, 14}, 10},
		{RangeInfo{10, 5}, 0}, // 颠倒视为空
	}
	for _, c := range cases {
		if got := c.r.Len(); got != c.want {
			t.Errorf("%+v.Len() = %d, want %d", c.r, got, c.want)
		}
	}
}
