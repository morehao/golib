package gprompt

// VarType 变量声明的取值类型。
//
// 类型只用于校验与文档化，不参与渲染时的隐式转换（对齐"不做静默转换"）。
type VarType string

const (
	VarTypeString VarType = "string" // 字符串
	VarTypeInt    VarType = "int"    // 整数
	VarTypeFloat  VarType = "float"  // 浮点数
	VarTypeBool   VarType = "bool"   // 布尔
	VarTypeList   VarType = "list"   // 列表（取值经 text/template 的 range 使用）
	VarTypeEnum   VarType = "enum"   // 枚举（取值必须命中 Enum 之一）
)

// valid 判断类型是否是受支持的声明类型。
func (t VarType) valid() bool {
	switch t {
	case VarTypeString, VarTypeInt, VarTypeFloat, VarTypeBool, VarTypeList, VarTypeEnum:
		return true
	default:
		return false
	}
}

// VariableSpec 一个模板变量的声明。
//
// 字段均为可选：Required 声明必填；Default 提供缺省值（Required 缺省时用）；
// Enum 仅对 VarTypeEnum 生效；Min/Max 用于数值范围；MinLen/MaxLen 用于字符串长度；
// MaxItems 用于列表元素个数上限。
type VariableSpec struct {
	Name     string
	Type     VarType
	Required bool
	Default  any
	Enum     []any
	// Min / Max 数值范围（int/float），nil 表示该侧不限制。
	Min, Max *float64
	// MinLen / MaxLen 字符串长度范围（string），nil 表示该侧不限制。
	MinLen, MaxLen *int
	// MaxItems 列表元素个数上限（list），nil 表示不限。
	MaxItems *int
}
