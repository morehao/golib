package gprompt

// PromptSnapshot 一次读取得到的「Prompt 基本信息 + 某一版本内容」快照。
//
// 它是 Source 与 PromptService 之间唯一的数据契约，与 gorm 实体解耦：
// DB Source / MemorySource 各自构造它，Service 只消费它做校验与渲染。
type PromptSnapshot struct {
	Code    string
	Name    string
	Status  Status
	Version int
	Content string
	// Variables 该版本声明的变量规格。
	Variables []VariableSpec
	Remark    string
}

// Status Prompt 状态。
type Status string

const (
	StatusEnabled  Status = "enabled"  // 启用
	StatusDisabled Status = "disabled" // 停用
)

func (s Status) valid() bool {
	return s == StatusEnabled || s == StatusDisabled
}
