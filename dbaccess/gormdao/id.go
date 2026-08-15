package gormdao

// IDType 主键 ID 类型约束：支持无符号/有符号整型与字符串。
// Dao 与 BaseCond 以该约束作为类型参数，从而一套代码同时支持 uint 与 string 主键。
type IDType interface {
	~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 |
		~int | ~int8 | ~int16 | ~int32 | ~int64 | ~string
}

// isZeroID 判断主键 ID 是否为零值：整型零值为 0，字符串零值为 ""。
// 联合约束的所有成员类型均 comparable，可直接使用 == 比较。
func isZeroID[ID IDType](v ID) bool {
	var zero ID
	return v == zero
}
