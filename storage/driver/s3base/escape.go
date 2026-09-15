package s3base

import "strings"

// S3 对 CopySource 等"值型路径参数"的处理是按查询串语义解码：值必须是 URL 编码的。
//
// 关键细节：url.PathEscape 不足以完成这件事。Go 认为 "+"、"&"、"@"、"="、":" 等
// 字符在路径段中是合法的，因此不会转义；而 S3 解码时会把 "+" 还原成空格，
// 于是 key 里的 "+" 会让 CopySource 指向一个不存在的对象。因此这里按 RFC 3986
// 只保留 unreserved 字符（ALPHA / DIGIT / "-" / "." / "_" / "~"），其余一律
// 百分号编码。

const upperhex = "0123456789ABCDEF"

func shouldEscapeByte(c byte, keepSlash bool) bool {
	switch {
	case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9':
		return false
	case c == '-', c == '.', c == '_', c == '~':
		return false
	case c == '/' && keepSlash:
		return false
	}
	return true
}

// escapeS3Value 按 RFC 3986 编码 s；keepSlash 为 true 时保留 "/"，用于表达 key 的层级。
func escapeS3Value(s string, keepSlash bool) string {
	needEscape := false
	for i := 0; i < len(s); i++ {
		if shouldEscapeByte(s[i], keepSlash) {
			needEscape = true
			break
		}
	}
	if !needEscape {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 8)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if shouldEscapeByte(c, keepSlash) {
			b.WriteByte('%')
			b.WriteByte(upperhex[c>>4])
			b.WriteByte(upperhex[c&0x0f])
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

// copySourceValue 构造 CopyObject 的 CopySource。
// bucket 名本身是 DNS 安全的，仍统一编码以保持一致；key 保留 "/" 表达目录层级。
func copySourceValue(bucket, key string) string {
	return escapeS3Value(bucket, false) + "/" + escapeS3Value(key, true)
}
