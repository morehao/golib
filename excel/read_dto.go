package excel

type ValidationError struct {
	DataRowNumber int    // 旧 API：0-based；v2 options 语义为 1-based。
	Head          string // Excel 表中的列名（即表头名）
	CellValue     string // Excel 表中的单元格值
	ExpectType    string // 期望的单元格类型
	ErrorMessage  string // 错误信息
}
