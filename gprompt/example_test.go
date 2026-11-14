package gprompt_test

import (
	"context"
	"fmt"

	"github.com/morehao/golib/gprompt"
)

// Example 演示用内存 Source 渲染一个带变量校验的 Prompt。
func Example() {
	ctx := context.Background()

	src := gprompt.NewMemorySource()
	_ = src.Put("greet", "问候", gprompt.StatusEnabled, 1, &gprompt.PromptSnapshot{
		Version: 1,
		Content: "你好 {{.name}}，欢迎来到 {{.city}}。",
		Variables: []gprompt.VariableSpec{
			{Name: "name", Type: gprompt.VarTypeString, Required: true},
			{Name: "city", Type: gprompt.VarTypeString, Required: true},
		},
	})

	svc, _ := gprompt.NewWithSource(src)
	out, err := svc.Render(ctx, "greet", map[string]any{"name": "李四", "city": "北京"})
	fmt.Println(out, err)

	// 缺必填变量 → 校验失败
	_, err = svc.Render(ctx, "greet", map[string]any{"name": "李四"})
	fmt.Println("missing:", err != nil)

	// Output:
	// 你好 李四，欢迎来到 北京。 <nil>
	// missing: true
}
