package gprompt

import (
	"errors"
	"strings"
	"testing"
)

// TestValidateAndRender 覆盖设计文档要求的六类场景：
// 引用未声明 / Required 缺 / 类型不符 / 枚举不符 / 范围越界 / 正常渲染。
func TestValidateAndRender(t *testing.T) {
	r := NewRenderer()

	float := func(f float64) *float64 { return &f }
	intp := func(i int) *int { return &i }

	t.Run("正常渲染", func(t *testing.T) {
		specs := []VariableSpec{
			{Name: "name", Type: VarTypeString, Required: true},
			{Name: "age", Type: VarTypeInt, Min: float(0), Max: float(150)},
		}
		out, err := r.ValidateAndRender(specs, "你好 {{.name}}，你 {{.age}} 岁", map[string]any{
			"name": "张三",
			"age":  30,
		})
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if out != "你好 张三，你 30 岁" {
			t.Fatalf("got %q", out)
		}
	})

	t.Run("引用未声明", func(t *testing.T) {
		specs := []VariableSpec{{Name: "name", Type: VarTypeString}}
		_, err := r.ValidateAndRender(specs, "hi {{.name}} {{.missing}}", map[string]any{"name": "x"})
		if !errors.Is(err, ErrUndeclaredVariable) {
			t.Fatalf("want ErrUndeclaredVariable, got %v", err)
		}
		if !strings.Contains(err.Error(), "missing") {
			t.Fatalf("error should mention missing, got %v", err)
		}
	})

	t.Run("Required缺失", func(t *testing.T) {
		specs := []VariableSpec{{Name: "name", Type: VarTypeString, Required: true}}
		_, err := r.ValidateAndRender(specs, "hi {{.name}}", map[string]any{})
		var errs Errors
		if !errors.As(err, &errs) {
			t.Fatalf("want Errors, got %T %v", err, err)
		}
		if errs.First().Field != "name" {
			t.Fatalf("want field name, got %v", errs.First())
		}
	})

	t.Run("类型不符", func(t *testing.T) {
		specs := []VariableSpec{{Name: "age", Type: VarTypeInt}}
		_, err := r.ValidateAndRender(specs, "age {{.age}}", map[string]any{"age": "not-int"})
		var errs Errors
		if !errors.As(err, &errs) {
			t.Fatalf("want Errors, got %v", err)
		}
		if errs.First().Field != "age" {
			t.Fatalf("want field age, got %v", errs.First())
		}
	})

	t.Run("枚举不符", func(t *testing.T) {
		specs := []VariableSpec{{Name: "level", Type: VarTypeEnum, Enum: []any{"low", "high"}}}
		_, err := r.ValidateAndRender(specs, "level {{.level}}", map[string]any{"level": "mid"})
		var errs Errors
		if !errors.As(err, &errs) {
			t.Fatalf("want Errors, got %v", err)
		}
		if errs.First().Field != "level" {
			t.Fatalf("want field level, got %v", errs.First())
		}
	})

	t.Run("范围越界", func(t *testing.T) {
		specs := []VariableSpec{{Name: "age", Type: VarTypeInt, Min: float(0), Max: float(150)}}
		_, err := r.ValidateAndRender(specs, "age {{.age}}", map[string]any{"age": 200})
		var errs Errors
		if !errors.As(err, &errs) {
			t.Fatalf("want Errors, got %v", err)
		}
		if errs.First().Field != "age" {
			t.Fatalf("want field age, got %v", errs.First())
		}
	})

	t.Run("字符串长度越界", func(t *testing.T) {
		specs := []VariableSpec{{Name: "name", Type: VarTypeString, MaxLen: intp(3)}}
		_, err := r.ValidateAndRender(specs, "{{.name}}", map[string]any{"name": "toolong"})
		var errs Errors
		if !errors.As(err, &errs) {
			t.Fatalf("want Errors, got %v", err)
		}
	})

	t.Run("列表超长", func(t *testing.T) {
		specs := []VariableSpec{{Name: "items", Type: VarTypeList, MaxItems: intp(2)}}
		_, err := r.ValidateAndRender(specs, "{{range .items}}{{.}}{{end}}", map[string]any{
			"items": []string{"a", "b", "c"},
		})
		var errs Errors
		if !errors.As(err, &errs) {
			t.Fatalf("want Errors, got %v", err)
		}
	})

	t.Run("默认值填充", func(t *testing.T) {
		specs := []VariableSpec{{Name: "greeting", Type: VarTypeString, Default: "hello"}}
		out, err := r.ValidateAndRender(specs, "{{.greeting}}", map[string]any{})
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if out != "hello" {
			t.Fatalf("got %q", out)
		}
	})

	t.Run("模板解析失败", func(t *testing.T) {
		_, err := r.ValidateAndRender(nil, "{{.name", map[string]any{})
		if !errors.Is(err, ErrTemplateParseFailed) {
			t.Fatalf("want ErrTemplateParseFailed, got %v", err)
		}
	})
}
