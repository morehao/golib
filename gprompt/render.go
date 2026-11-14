package gprompt

import (
	"bytes"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strings"
	"text/template"
	"text/template/parse"
)

// FieldError 单字段的校验错误，字段名 + 描述。
//
// 与 gvalidate.FieldError 保持同样的对外形状（Field/Message + Error() 返回 Message），
// 使调用方可以统一以"字段名 → 消息"的方式呈现，同时不把 gprompt 与 gvalidate 耦合成
// 依赖（gprompt 允许零校验器独立使用）。
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

func (e *FieldError) Error() string { return e.Message }

// Errors 一组字段错误，实现 error 便于整体返回。
type Errors []*FieldError

func (e Errors) Error() string {
	if len(e) == 0 {
		return ""
	}
	return e[0].Message
}

// First 返回第一个字段错误，空时返回 nil。
func (e Errors) First() *FieldError {
	if len(e) == 0 {
		return nil
	}
	return e[0]
}

// ToMap 转成 field->message 映射，便于 API 层直接返回。
func (e Errors) ToMap() map[string]string {
	m := make(map[string]string, len(e))
	for _, fe := range e {
		m[fe.Field] = fe.Message
	}
	return m
}

// Renderer 纯渲染 + 变量校验组件，无 IO、无状态，可安全共享。
//
// 典型用法：ValidateAndRender(specs, tmpl, values) → 校验 → text/template 渲染。
type Renderer struct{}

// NewRenderer 构造一个 Renderer（无状态，值语义即可）。
func NewRenderer() *Renderer { return &Renderer{} }

// ValidateAndRender 校验变量并渲染模板。
//
// 处理顺序：
//  1. 解析模板（parse 失败 → ErrTemplateParseFailed）；从模板 AST 收集所有被引用的变量名，
//     与声明做双向核对：引用了未声明的变量 → ErrUndeclaredVariable；
//     Required 变量在 values 与 Default 中都缺 → ErrMissingRequiredVar。
//  2. 按 spec 逐项校验类型/范围/枚举/长度（错误聚合为 Errors，逐字段带名）。
//  3. 用 text/template 以 missingkey=error 兜底渲染（渲染失败 → ErrTemplateRenderFailed）。
//
// values 会被补齐 Default（不修改入参 map）；未声明但 values 里多出来的键不报错（前向兼容）。
func (r *Renderer) ValidateAndRender(specs []VariableSpec, tmpl string, values map[string]any) (string, error) {
	parsed, err := template.New("prompt").Option("missingkey=error").Parse(tmpl)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrTemplateParseFailed, err)
	}

	specByName := make(map[string]VariableSpec, len(specs))
	for _, s := range specs {
		if !s.Type.valid() {
			return "", fmt.Errorf("%w: variable %q has unknown type %q", ErrInvalidValue, s.Name, s.Type)
		}
		specByName[s.Name] = s
	}

	// 1a. 模板引用的变量必须已声明。
	if undeclared := undeclaredRefs(parsed, specByName); len(undeclared) > 0 {
		return "", fmt.Errorf("%w: %s", ErrUndeclaredVariable, strings.Join(undeclared, ", "))
	}

	// 1b. Required 缺失（values 与 Default 都没有）→ 报字段错。
	merged := make(map[string]any, len(values)+len(specs))
	for k, v := range values {
		merged[k] = v
	}
	var errs Errors
	for _, s := range specs {
		if _, ok := merged[s.Name]; !ok {
			if s.Default != nil {
				merged[s.Name] = s.Default
				continue
			}
			if s.Required {
				errs = append(errs, &FieldError{Field: s.Name, Message: fmt.Sprintf("variable %q is required", s.Name)})
			}
		}
	}

	// 2. 逐项校验类型/范围/枚举/长度。
	for _, s := range specs {
		val, ok := merged[s.Name]
		if !ok {
			continue // 非必填且未提供：允许为空，模板侧靠 missingkey 兜底
		}
		if fe := validateValue(s, val); fe != nil {
			errs = append(errs, fe)
		}
	}
	if len(errs) > 0 {
		return "", errs
	}

	// 3. 渲染。
	var buf bytes.Buffer
	if err := parsed.Execute(&buf, merged); err != nil {
		return "", fmt.Errorf("%w: %v", ErrTemplateRenderFailed, err)
	}
	return buf.String(), nil
}

// undeclaredRefs 收集模板中引用、但没有对应声明的变量名（排序稳定输出）。
//
// 只收集裸变量引用（NodeField 的标识符根，如 {{.name}} 的 name）；对 {{range .items}}
// 的字段访问只记根变量名 items，符合"变量声明是平的"这一约定。
func undeclaredRefs(tmpl *template.Template, specByName map[string]VariableSpec) []string {
	seen := make(map[string]struct{})
	var walk func(list *parse.ListNode)
	walk = func(list *parse.ListNode) {
		if list == nil {
			return
		}
		for _, n := range list.Nodes {
			switch node := n.(type) {
			case *parse.ActionNode:
				collectRefs(node.Pipe, seen)
			case *parse.IfNode:
				walk(node.List)
				walk(node.ElseList)
			case *parse.RangeNode:
				walk(node.List)
				walk(node.ElseList)
			case *parse.WithNode:
				walk(node.List)
				walk(node.ElseList)
			case *parse.TemplateNode:
				if node.Pipe != nil {
					collectRefs(node.Pipe, seen)
				}
			}
		}
	}
	walk(tmpl.Tree.Root)

	undeclared := make([]string, 0, len(seen))
	for name := range seen {
		if _, ok := specByName[name]; !ok {
			undeclared = append(undeclared, name)
		}
	}
	sort.Strings(undeclared)
	return undeclared
}

// collectRefs 从一条 pipeline 表达式中收集裸变量名（标识符根）。
func collectRefs(pipe *parse.PipeNode, seen map[string]struct{}) {
	if pipe == nil {
		return
	}
	for _, cmd := range pipe.Cmds {
		for _, arg := range cmd.Args {
			collectRefFromArg(arg, seen)
		}
	}
}

func collectRefFromArg(arg parse.Node, seen map[string]struct{}) {
	switch a := arg.(type) {
	case *parse.FieldNode:
		// Field 的 Ident 是路径段切片，如 .a.b → ["a","b"]；取根段作为变量名。
		if len(a.Ident) == 0 {
			return
		}
		name := a.Ident[0]
		if name != "" && !isKeywordField(name) {
			seen[name] = struct{}{}
		}
	case *parse.ChainNode:
		collectRefFromArg(a.Node, seen)
	case *parse.PipeNode:
		collectRefs(a, seen)
	}
}

// isKeywordField 排除 text/template 的内建"点字段"（如 . 本身）。
func isKeywordField(name string) bool { return name == "" }

// validateValue 校验单个变量的取值，通过返回 nil。
func validateValue(s VariableSpec, val any) *FieldError {
	bad := func(format string, args ...any) *FieldError {
		return &FieldError{Field: s.Name, Message: fmt.Sprintf(format, args...)}
	}

	switch s.Type {
	case VarTypeString:
		sv, ok := val.(string)
		if !ok {
			return bad("variable %q expects string, got %s", s.Name, typeName(val))
		}
		n := len([]rune(sv))
		if s.MinLen != nil && n < *s.MinLen {
			return bad("variable %q length %d < min %d", s.Name, n, *s.MinLen)
		}
		if s.MaxLen != nil && n > *s.MaxLen {
			return bad("variable %q length %d > max %d", s.Name, n, *s.MaxLen)
		}
		return nil

	case VarTypeInt:
		iv, ok := toInt(val)
		if !ok {
			return bad("variable %q expects int, got %s", s.Name, typeName(val))
		}
		return checkRange(s, float64(iv), bad)

	case VarTypeFloat:
		fv, ok := toFloat(val)
		if !ok {
			return bad("variable %q expects float, got %s", s.Name, typeName(val))
		}
		return checkRange(s, fv, bad)

	case VarTypeBool:
		if _, ok := val.(bool); !ok {
			return bad("variable %q expects bool, got %s", s.Name, typeName(val))
		}
		return nil

	case VarTypeList:
		rv := reflect.ValueOf(val)
		if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
			return bad("variable %q expects list, got %s", s.Name, typeName(val))
		}
		if s.MaxItems != nil && rv.Len() > *s.MaxItems {
			return bad("variable %q has %d items > max %d", s.Name, rv.Len(), *s.MaxItems)
		}
		return nil

	case VarTypeEnum:
		if len(s.Enum) == 0 {
			return bad("variable %q is enum type but declares no enum values", s.Name)
		}
		for _, e := range s.Enum {
			if equalValue(e, val) {
				return nil
			}
		}
		return bad("variable %q value %v is not in enum %v", s.Name, val, s.Enum)

	default:
		return bad("variable %q has unknown type %q", s.Name, s.Type)
	}
}

func checkRange(s VariableSpec, fv float64, bad func(string, ...any) *FieldError) *FieldError {
	if s.Min != nil && fv < *s.Min {
		return bad("variable %q value %v < min %v", s.Name, fv, *s.Min)
	}
	if s.Max != nil && fv > *s.Max {
		return bad("variable %q value %v > max %v", s.Name, fv, *s.Max)
	}
	return nil
}

// toInt 把整数类取值收敛为 int64；浮点若为整值也接受（避免 JSON 数字统一 float64 的坑）。
func toInt(val any) (int64, bool) {
	switch v := val.(type) {
	case int:
		return int64(v), true
	case int8:
		return int64(v), true
	case int16:
		return int64(v), true
	case int32:
		return int64(v), true
	case int64:
		return v, true
	case uint:
		return int64(v), true
	case uint8:
		return int64(v), true
	case uint16:
		return int64(v), true
	case uint32:
		return int64(v), true
	case uint64:
		if v > math.MaxInt64 {
			return 0, false
		}
		return int64(v), true
	case float32:
		return floatToInt(float64(v))
	case float64:
		return floatToInt(v)
	default:
		return 0, false
	}
}

func floatToInt(f float64) (int64, bool) {
	if math.Trunc(f) != f || math.IsInf(f, 0) || math.IsNaN(f) {
		return 0, false
	}
	return int64(f), true
}

// toFloat 把数值类取值收敛为 float64。
func toFloat(val any) (float64, bool) {
	switch v := val.(type) {
	case float32:
		return float64(v), true
	case float64:
		return v, true
	case int:
		return float64(v), true
	case int8:
		return float64(v), true
	case int16:
		return float64(v), true
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	case uint:
		return float64(v), true
	case uint8:
		return float64(v), true
	case uint16:
		return float64(v), true
	case uint32:
		return float64(v), true
	case uint64:
		return float64(v), true
	default:
		return 0, false
	}
}

// equalValue 用于枚举命中判断：优先 reflect.DeepEqual，数值类跨类型按值比较。
func equalValue(a, b any) bool {
	if reflect.DeepEqual(a, b) {
		return true
	}
	if af, ok := toFloat(a); ok {
		if bf, ok2 := toFloat(b); ok2 {
			return af == bf
		}
	}
	return false
}

// typeName 返回取值的可读类型名，用于错误信息。
func typeName(v any) string {
	if v == nil {
		return "nil"
	}
	return reflect.TypeOf(v).String()
}
