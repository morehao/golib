package ginmiddleware

import (
	"mime"
	"net/url"
	"regexp"
	"strings"
)

// 本文件负责访问日志内容的脱敏：按"字段名模式"匹配 JSON 键与 form/query 参数名，
// 只替换值、保留结构与键名，因此日志仍然可读、可定位。这与 Elastic APM 的
// sanitize_field_names、Datadog 的 obfuscation 规则是同一类做法（主流 APM 均按字段名
// 脱敏，而不是按值正则去猜）。
//
// 明确不做的事：按"值"的模式（手机号、身份证号、邮箱等）脱敏。值正则无法区分
// 业务含义，容易把正常内容改坏；需要时应在业务侧决定哪些字段值得记录。

// DefaultMaskText 是默认的替换文本。
const DefaultMaskText = "***"

// DefaultSensitiveFields 是默认脱敏的字段名模式：glob（* 通配）、大小写不敏感，
// 同时用于匹配 JSON 键名与 form/query 参数名。
//
// 模式参考 Elastic APM 的默认 sanitize_field_names，并补充了常见的中文业务字段命名
// （idCard/phone/mobile/bankCard 等）。宁可略微过度匹配，也不要漏掉凭据类字段。
var DefaultSensitiveFields = []string{
	"password", "*password*",
	"passwd", "*passwd*",
	"pwd", "*pwd*",
	"secret", "*secret*",
	"token", "*token*",
	"credential", "*credential*",
	"api_key", "*api_key*", "apikey", "*apikey*",
	"access_key", "*access_key*", "access_key_id", "access_key_secret",
	"private_key", "*private_key*",
	"authorization", "*authorization*",
	"cookie", "*cookie*",
	"session", "*session*",
	"jwt", "*jwt*",
	"signature", "*signature*",
	"sign",
	"id_card", "*id_card*", "idcard", "*idcard*", "id_no", "idno",
	"phone", "*phone*", "mobile", "*mobile*", "telephone", "*telephone*",
	"email", "*email*",
	"bank_card", "*bank_card*", "bankcard", "*bankcard*",
	"credit_card", "*credit_card*", "creditcard", "*creditcard*",
	"cvv", "cvc",
	"ssn", "passport", "*passport*",
}

// SanitizePolicy 描述访问日志内容的脱敏策略。
type SanitizePolicy struct {
	// Fields 为敏感字段名模式（glob，大小写不敏感）。nil 表示使用 DefaultSensitiveFields；
	// 空切片表示完全不脱敏（等价于 WithoutSanitize）。
	Fields []string
	// Mask 为替换后的文本，默认 DefaultMaskText。请使用不含引号、&、= 的简单文本，
	// 以免破坏被记录的文本结构。
	Mask string
}

type sanitizer struct {
	// keyRe 匹配 form/query 的完整参数名（^...$）。
	keyRe *regexp.Regexp
	// jsonRe 匹配 JSON 中的 "键": 值，捕获组为 键 / 分隔符 / 值。
	jsonRe      *regexp.Regexp
	jsonReplace string
	mask        string
}

// newSanitizer 编译脱敏规则；策略关闭（Fields 为空切片）或没有可用模式时返回 nil，
// 调用方可以直接在 nil 上调用方法（全部是空操作）。
func newSanitizer(policy SanitizePolicy) *sanitizer {
	fields := policy.Fields
	if fields == nil {
		fields = DefaultSensitiveFields
	}
	mask := policy.Mask
	if mask == "" {
		mask = DefaultMaskText
	}

	var keyAlts, jsonAlts []string
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		// 参数名里可能出现 user.password、password[] 之类的形态，键匹配用 .* 更宽容；
		// JSON 键是字符串，* 不能跨过引号，用 [^"]* 更精确。
		keyAlts = append(keyAlts, globToRegex(field, ".*"))
		jsonAlts = append(jsonAlts, globToRegex(field, `[^"]*`))
	}
	if len(keyAlts) == 0 {
		return nil
	}

	keyPattern := `(?i)^(?:` + strings.Join(keyAlts, "|") + `)$`
	// 值只匹配"字符串"与"标量"两种形态：对象/数组不做替换（正则无法可靠地配对括号，
	// 强行替换会破坏日志文本）。敏感键在对象内部时仍会被单独匹配到。
	jsonPattern := `(?i)"(` + strings.Join(jsonAlts, "|") + `)"([ \t]*:[ \t]*)("(?:[^"\\]|\\.)*"|[^,{}\[\]\s]+)`
	replace := `"${1}"${2}"` + strings.ReplaceAll(mask, "$", "$$") + `"`

	return &sanitizer{
		keyRe:       regexp.MustCompile(keyPattern),
		jsonRe:      regexp.MustCompile(jsonPattern),
		jsonReplace: replace,
		mask:        mask,
	}
}

// globToRegex 把 glob 模式转为正则片段，* 用 wildcard 指定的片段替换。
func globToRegex(pattern, wildcard string) string {
	var b strings.Builder
	for _, r := range pattern {
		if r == '*' {
			b.WriteString(wildcard)
			continue
		}
		b.WriteString(regexp.QuoteMeta(string(r)))
	}
	return b.String()
}

// form 对 a=1&b=2 形态的 query / form-urlencoded 内容按参数名脱敏。
func (s *sanitizer) form(raw string) string {
	if s == nil || raw == "" {
		return raw
	}
	parts := strings.Split(raw, "&")
	changed := false
	for i, part := range parts {
		eq := strings.IndexByte(part, '=')
		if eq <= 0 {
			continue
		}
		key := part[:eq]
		if unescaped, err := url.QueryUnescape(key); err == nil {
			key = unescaped
		}
		if !s.keyRe.MatchString(key) {
			continue
		}
		parts[i] = part[:eq+1] + s.mask
		changed = true
	}
	if !changed {
		return raw
	}
	return strings.Join(parts, "&")
}

// json 对 JSON 文本按字段名脱敏。只做文本替换，不重新序列化：既不改变键序与数字写法，
// 也能作用于被采集上限截断的片段。
func (s *sanitizer) json(content string) string {
	if s == nil || content == "" {
		return content
	}
	return s.jsonRe.ReplaceAllString(content, s.jsonReplace)
}

// body 按内容类型选择脱敏方式：JSON 走 json、form-urlencoded 走 form，其余原样返回。
// 内容类型缺失或与实际不符时，用内容是否以 { / [ 开头来兜底识别 JSON。
func (s *sanitizer) body(content, contentType string) string {
	if s == nil || content == "" {
		return content
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType = contentType
	}
	mediaType = strings.ToLower(strings.TrimSpace(mediaType))

	if mediaType == "application/x-www-form-urlencoded" {
		return s.form(content)
	}
	if strings.Contains(mediaType, "json") || isJSONLike(content) {
		return s.json(content)
	}
	return content
}

// fullURL 返回脱敏后的完整 URL：只替换 query 部分，未发生脱敏时返回原值。
func (s *sanitizer) fullURL(u *url.URL) string {
	if u == nil {
		return ""
	}
	if s == nil || u.RawQuery == "" {
		return u.String()
	}
	sanitized := s.form(u.RawQuery)
	if sanitized == u.RawQuery {
		return u.String()
	}
	// 复制一份再改写，避免影响 ctx 中的原始 URL（业务可能依赖它）。
	clone := *u
	clone.RawQuery = sanitized
	return clone.String()
}

func isJSONLike(content string) bool {
	trimmed := strings.TrimLeft(content, " \t\r\n")
	return strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[")
}
