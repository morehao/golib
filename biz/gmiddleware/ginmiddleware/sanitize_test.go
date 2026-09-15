package ginmiddleware

import (
	"net/url"
	"testing"
)

func TestSanitizerJSON(t *testing.T) {
	s := newSanitizer(SanitizePolicy{})

	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "按字段名替换值",
			in:   `{"user":"bob","password":"p@ss"}`,
			want: `{"user":"bob","password":"***"}`,
		},
		{
			name: "嵌套字段",
			in:   `{"data":{"token":"abc","page":1}}`,
			want: `{"data":{"token":"***","page":1}}`,
		},
		{
			name: "大小写不敏感",
			in:   `{"PassWord":"x"}`,
			want: `{"PassWord":"***"}`,
		},
		{
			name: "glob 匹配驼峰命名",
			in:   `{"accessToken":"x","refresh_token":"y"}`,
			want: `{"accessToken":"***","refresh_token":"***"}`,
		},
		{
			name: "非字符串值也被替换",
			in:   `{"phone":13800138000}`,
			want: `{"phone":"***"}`,
		},
		{
			name: "中文业务字段",
			in:   `{"idCard":"110101199001011234","mobile":"13800138000"}`,
			want: `{"idCard":"***","mobile":"***"}`,
		},
		{
			name: "不相关字段保持不变",
			in:   `{"code":0,"msg":"success","requestID":"abc"}`,
			want: `{"code":0,"msg":"success","requestID":"abc"}`,
		},
		{
			name: "转义引号不影响键识别",
			in:   `{"password":"a\"b","user":"c"}`,
			want: `{"password":"***","user":"c"}`,
		},
		{
			name: "被截断的字符串值也能替换",
			in:   `{"password":"abc`,
			want: `{"password":"***"`,
		},
		{
			// 文档化的限制：值本身是对象/数组时不做替换，避免正则破坏文本结构；
			// 但对象内部的敏感键仍会被单独匹配到。
			name: "敏感键的值是对象时内部敏感键仍被替换",
			in:   `{"credentials":{"password":"x","host":"h"}}`,
			want: `{"credentials":{"password":"***","host":"h"}}`,
		},
		{
			name: "空内容",
			in:   "",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := s.json(tt.in); got != tt.want {
				t.Fatalf("json() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestSanitizerForm(t *testing.T) {
	s := newSanitizer(SanitizePolicy{})

	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "按参数名替换值", in: "user=bob&password=p%40ss", want: "user=bob&password=***"},
		{name: "下划线风格", in: "access_token=abc&page=1", want: "access_token=***&page=1"},
		{name: "url 编码的参数名", in: "user%2Epassword=abc", want: "user%2Epassword=***"},
		{name: "无值参数保持不变", in: "flag&page=1", want: "flag&page=1"},
		{name: "非敏感参数保持不变", in: "page=1&size=20", want: "page=1&size=20"},
		{name: "空串", in: "", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := s.form(tt.in); got != tt.want {
				t.Fatalf("form() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSanitizerBody(t *testing.T) {
	s := newSanitizer(SanitizePolicy{})

	tests := []struct {
		name        string
		content     string
		contentType string
		want        string
	}{
		{
			name: "json 内容类型", contentType: "application/json; charset=utf-8",
			content: `{"password":"x"}`, want: `{"password":"***"}`,
		},
		{
			name: "+json 后缀类型", contentType: "application/vnd.api+json",
			content: `{"token":"x"}`, want: `{"token":"***"}`,
		},
		{
			name: "未声明内容类型时按内容兜底识别 json", contentType: "",
			content: `{"password":"x"}`, want: `{"password":"***"}`,
		},
		{
			name: "form 内容类型", contentType: "application/x-www-form-urlencoded",
			content: "password=x&user=bob", want: "password=***&user=bob",
		},
		{
			name: "纯文本不脱敏", contentType: "text/plain",
			content: "password=x", want: "password=x",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := s.body(tt.content, tt.contentType); got != tt.want {
				t.Fatalf("body() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSanitizerFullURL(t *testing.T) {
	s := newSanitizer(SanitizePolicy{})

	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "只改写 query", raw: "/v1/app/users?access_token=abc&page=1", want: "/v1/app/users?access_token=***&page=1"},
		{name: "无敏感参数时原样返回", raw: "/v1/app/users?page=1", want: "/v1/app/users?page=1"},
		{name: "无 query", raw: "/v1/app/users", want: "/v1/app/users"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u, err := url.Parse(tt.raw)
			if err != nil {
				t.Fatal(err)
			}
			if got := s.fullURL(u); got != tt.want {
				t.Fatalf("fullURL() = %q, want %q", got, tt.want)
			}
			if u.RawQuery != rawQueryOf(tt.raw) {
				t.Fatalf("原始 URL 被改写: %q", u.RawQuery)
			}
		})
	}

	if got := s.fullURL(nil); got != "" {
		t.Fatalf("fullURL(nil) = %q, want empty", got)
	}
}

// rawQueryOf 返回 raw 中 ? 之后的部分（无 ? 时为空），用于校验原始 URL 未被改写。
func rawQueryOf(raw string) string {
	for i := 0; i < len(raw); i++ {
		if raw[i] == '?' {
			return raw[i+1:]
		}
	}
	return ""
}

// 关闭脱敏时 newSanitizer 返回 nil，所有方法都是空操作。
func TestSanitizerDisabled(t *testing.T) {
	s := newSanitizer(SanitizePolicy{Fields: []string{}})
	if s != nil {
		t.Fatal("Fields 为空切片时应返回 nil sanitizer")
	}

	if got := s.json(`{"password":"x"}`); got != `{"password":"x"}` {
		t.Fatalf("nil sanitizer json() = %q", got)
	}
	if got := s.form("password=x"); got != "password=x" {
		t.Fatalf("nil sanitizer form() = %q", got)
	}
	if got := s.body(`{"password":"x"}`, "application/json"); got != `{"password":"x"}` {
		t.Fatalf("nil sanitizer body() = %q", got)
	}
}

func TestSanitizerCustomPolicy(t *testing.T) {
	s := newSanitizer(SanitizePolicy{Fields: []string{"userName"}, Mask: "[HIDDEN]"})

	if got := s.json(`{"userName":"bob","password":"p"}`); got != `{"userName":"[HIDDEN]","password":"p"}` {
		t.Fatalf("自定义字段 json() = %q", got)
	}
	if got := s.form("userName=bob&password=p"); got != "userName=[HIDDEN]&password=p" {
		t.Fatalf("自定义字段 form() = %q", got)
	}
}

// Mask 中的 $ 在替换模板里有特殊含义，必须按字面量输出。
func TestSanitizerMaskWithDollar(t *testing.T) {
	s := newSanitizer(SanitizePolicy{Mask: "$1"})

	if got := s.json(`{"password":"x"}`); got != `{"password":"$1"}` {
		t.Fatalf("Mask 含 $ 时 json() = %q", got)
	}
}
