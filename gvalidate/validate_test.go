package gvalidate

import (
	"strings"
	"testing"
)

type requiredEmailDTO struct {
	Name  string `validate:"required" label:"名称"`
	Email string `validate:"email" json:"email"`
}

// TestCheck_Valid 通过校验应返回 nil。
func TestCheck_Valid(t *testing.T) {
	dto := &requiredEmailDTO{Name: "gopher", Email: "a@b.com"}
	if err := Check(dto); err != nil {
		t.Fatalf("Check() = %v, want nil", err)
	}
}

// TestCheck_ChineseTranslatorWorks 回归测试：中文翻译器必须可用。
// 修复前 init 会失败，Check() 对任意入参都返回
// "validate: translator zh_Hans_CN not found"，中文校验整体失效。
func TestCheck_ChineseTranslatorWorks(t *testing.T) {
	dto := &requiredEmailDTO{} // Name 为空触发 required，Email 为空触发 email
	err := Check(dto)
	if err == nil {
		t.Fatal("Check() = nil, want validation errors")
	}
	switch err.Error() {
	case "validate: translator zh_Hans_CN not found":
		t.Fatalf("中文翻译器初始化失败（回归）: %v", err)
	case "validate: register translations":
		t.Fatalf("注册中文翻译失败（回归）: %v", err)
	}

	errs, ok := AsErrors(err)
	if !ok {
		t.Fatalf("AsErrors() ok=false, err=%v（中文校验器未生效）", err)
	}
	byField := errs.ToMap()
	if msg := byField["名称"]; msg == "" || !strings.Contains(msg, "必填") {
		t.Errorf("Name required 中文消息异常: %q", msg)
	}
	if msg := byField["email"]; msg == "" || !strings.Contains(msg, "邮箱") {
		t.Errorf("Email 中文消息异常: %q", msg)
	}
}
