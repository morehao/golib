package gllm

// Capability 声明一个驱动类型支持的能力。
//
// 阶段一实际只用 chat 本体；Tools/Vision/Reasoning 用于启动期校验与运行期查询，
// 让调用方能提前判断「这个 provider 能不能走工具调用」，而不是等到运行期报错。
type Capability struct {
	// Tools 表示支持 tool calling / function calling。
	Tools bool
	// Vision 表示支持图片等多模态输入。
	Vision bool
	// Reasoning 表示会返回 reasoning/thinking 内容。
	Reasoning bool
}

// Supports 报告 c 是否覆盖 want 的所有能力位。want 的零值恒为真。
func (c Capability) Supports(want Capability) bool {
	return (!want.Tools || c.Tools) &&
		(!want.Vision || c.Vision) &&
		(!want.Reasoning || c.Reasoning)
}

// Capabilities 返回某个已注册驱动声明的能力。
// 第二返回值为 false 表示该类型未注册。
func Capabilities(driverType string) (Capability, bool) {
	e, ok := driverInfo(driverType)
	if !ok {
		return Capability{}, false
	}
	return e.capability, true
}

// Supports 报告某驱动类型是否支持给定能力。类型未注册时返回 false。
func Supports(driverType string, want Capability) bool {
	c, ok := Capabilities(driverType)
	return ok && c.Supports(want)
}
