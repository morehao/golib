package gutil

import (
	"fmt"
	"testing"
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
