package gutil

import (
	"fmt"
	"testing"
	"time"
)

func TestGetDate(t *testing.T) {
	fmt.Println(GetDate(0))
}

func TestGetThisWeekRange(t *testing.T) {
	fmt.Println(GetThisWeekRange())
}

func TestGetMonthRange(t *testing.T) {
	fmt.Println(GetMonthRange(1))
}

func TestGetYearRange(t *testing.T) {
	fmt.Println(GetYearRange(1))
}

// TestGetThisMonthRange 验证返回的是「当月首末日」而不是周区间。
func TestGetThisMonthRange(t *testing.T) {
	now := time.Now()
	wantStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.Local).Format(YYYYMMDD)
	wantEnd := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.Local).
		AddDate(0, 1, -1).Format(YYYYMMDD)

	gotStart, gotEnd := GetThisMonthRange()
	if gotStart != wantStart || gotEnd != wantEnd {
		t.Fatalf("GetThisMonthRange() = (%s, %s), want (%s, %s)", gotStart, gotEnd, wantStart, wantEnd)
	}
}

// TestGetLastMonthRange 验证返回的是「上月首末日」而不是周区间。
func TestGetLastMonthRange(t *testing.T) {
	now := time.Now()
	lastMonth := now.AddDate(0, -1, 0)
	first := time.Date(lastMonth.Year(), lastMonth.Month(), 1, 0, 0, 0, 0, time.Local)
	wantStart := first.Format(YYYYMMDD)
	wantEnd := first.AddDate(0, 1, -1).Format(YYYYMMDD)

	gotStart, gotEnd := GetLastMonthRange()
	if gotStart != wantStart || gotEnd != wantEnd {
		t.Fatalf("GetLastMonthRange() = (%s, %s), want (%s, %s)", gotStart, gotEnd, wantStart, wantEnd)
	}

	// 额外断言：区间跨度必须是该月的天数（28-31），而不是一周的 7 天。
	start, err := time.ParseInLocation(YYYYMMDD, gotStart, time.Local)
	if err != nil {
		t.Fatalf("parse start: %v", err)
	}
	end, err := time.ParseInLocation(YYYYMMDD, gotEnd, time.Local)
	if err != nil {
		t.Fatalf("parse end: %v", err)
	}
	days := int(end.Sub(start).Hours()/24) + 1
	if days < 28 || days > 31 {
		t.Fatalf("last month range spans %d days, want within [28,31]", days)
	}
}

