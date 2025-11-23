package gutil

import (
	"time"
)

const (
	YYYY_MM_DD_HH_MM_SS = "2006-01-02 15:04:05"
	YYYY_MM_DD          = "2006-01-02"
	MM_DD               = "01-02"
	YYYY                = "2006"
	YYYYMMDD            = "20060102"
	YYYYMM              = "200601"
	MMDD                = "0102"

	DayDuration = 24 * time.Hour
)

func TimeFormat(t time.Time, format string) string {
	if t.Unix() <= 0 {
		return ""
	}
	return t.Format(format)
}

func GetToday() string {
	return time.Now().Format(YYYYMMDD)
}

func GetLastDay() string {
	return time.Now().Add(-1 * DayDuration).Format(YYYYMMDD)
}

func GetDate(offset int) string {
	return time.Now().AddDate(0, 0, offset).Format(YYYYMMDD)
}

// GetWeekRange 获取本周的起始和结束日期，返回示例：20230901, 20230907
// offset: 0 表示本周，-7 表示上周
func GetWeekRange(offset int) (string, string) {
	now := time.Now().Add(time.Duration(offset) * DayDuration)
	weekDay := int(now.Weekday())
	if weekDay == 0 {
		weekDay = 7 // 将周日视为一周的最后一天
	}

	// 获取周起始和结束日期
	weekStart := now.Add(time.Duration(-weekDay+1) * DayDuration)
	weekEnd := weekStart.Add(6 * DayDuration)

	// 返回格式化字符串
	return weekStart.Format(YYYYMMDD), weekEnd.Format(YYYYMMDD)
}

// GetThisWeekRange 获取本周的起始和结束日期，如 20230901, 20230907
func GetThisWeekRange() (string, string) {
	return GetWeekRange(0)
}

// GetLastWeekRange 获取上周的起始和结束日期，如 20230824, 20230830
func GetLastWeekRange() (string, string) {
	return GetWeekRange(-7)
}

func GetMonth(offset int) string {
	now := time.Now()
	startDate := now.AddDate(offset, 0, 0)
	return startDate.Format(YYYYMM)
}

// GetMonthRange 获取指定偏移月的起始日期和结束日期，如 20230901, 20230930
// offset: 相对于当前月的偏移量，0 表示当前月，-1 表示上个月，1 表示下个月
func GetMonthRange(offset int) (string, string) {
	now := time.Now()

	// 获取偏移月后的时间
	startDate := now.AddDate(0, offset, 0) // 计算目标月的开始日期

	// 获取当月的起始日期（第一天）
	startOfMonth := time.Date(startDate.Year(), startDate.Month(), 1, 0, 0, 0, 0, time.Local)

	// 获取当月的结束日期（最后一天）
	endOfMonth := startOfMonth.AddDate(0, 1, -1)

	return startOfMonth.Format(YYYYMMDD), endOfMonth.Format(YYYYMMDD)
}

func GetThisMonthRange() (string, string) {
	return GetWeekRange(0)
}

func GetLastMonthRange() (string, string) {
	return GetWeekRange(-1)
}

func GetYear(offset int) string {
	now := time.Now()
	return now.AddDate(offset, 0, 0).Format(YYYY)
}

func GetYearRange(offset int) (string, string) {
	now := time.Now()
	startDate := now.AddDate(offset, 0, 0)
	startOfYear := time.Date(startDate.Year(), 1, 1, 0, 0, 0, 0, time.Local)
	endOfYear := time.Date(startDate.Year(), 12, 31, 0, 0, 0, 0, time.Local)
	return startOfYear.Format(YYYYMMDD), endOfYear.Format(YYYYMMDD)
}

func GetThisYearRange() (string, string) {
	return GetYearRange(0)
}

func GetLastYearRange() (string, string) {
	return GetYearRange(-1)
}
