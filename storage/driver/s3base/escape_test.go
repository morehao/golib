package s3base

import (
	"net/url"
	"testing"
)

func TestEscapeS3Value(t *testing.T) {
	cases := []struct {
		name      string
		in        string
		keepSlash bool
		want      string
	}{
		{"unreserved passthrough", "-_.~AZaz09", true, "-_.~AZaz09"},
		{"slash kept", "a/b/c.txt", true, "a/b/c.txt"},
		{"slash escaped", "a/b/c.txt", false, "a%2Fb%2Fc.txt"},
		{"plus escaped", "a+b.txt", true, "a%2Bb.txt"},
		{"space escaped", "a b.txt", true, "a%20b.txt"},
		{"hash escaped", "a#b.txt", true, "a%23b.txt"},
		{"percent escaped", "a%b.txt", true, "a%25b.txt"},
		{"query chars escaped", "a&b=c?d.txt", true, "a%26b%3Dc%3Fd.txt"},
		{"at and colon escaped", "a@b:c.txt", true, "a%40b%3Ac.txt"},
		{"non-ascii escaped", "中文.txt", true, "%E4%B8%AD%E6%96%87.txt"},
		{"empty", "", true, ""},
	}
	for _, c := range cases {
		if got := escapeS3Value(c.in, c.keepSlash); got != c.want {
			t.Errorf("%s: escapeS3Value(%q, %v) = %q, want %q", c.name, c.in, c.keepSlash, got, c.want)
		}
	}
}

func TestCopySourceValue(t *testing.T) {
	got := copySourceValue("mybucket", "dir/a+b c#d.txt")
	want := "mybucket/dir/a%2Bb%20c%23d.txt"
	if got != want {
		t.Fatalf("copySourceValue = %q, want %q", got, want)
	}
}

// 这条断言记录了"为什么不能用 url.PathEscape"：
// Go 视 "+" 为合法路径字符而不转义，但 S3 按查询串语义解码 CopySource，
// "+" 会被还原成空格，导致指向不存在的对象。
// 若 Go 的标准库行为改变，本测试会失败以提醒重新评估转义实现。
func TestEscapeS3Value_PlusIsWhyPathEscapeIsInsufficient(t *testing.T) {
	if url.PathEscape("a+b") != "a+b" {
		t.Fatal("url.PathEscape 现在会转义 '+': 请重新评估是否需要自定义转义")
	}
	if url.PathEscape("a&b=c") != "a&b=c" {
		t.Fatal("url.PathEscape 现在会转义 '&'/'=': 请重新评估是否需要自定义转义")
	}
	if got := escapeS3Value("a+b&c=d", true); got != "a%2Bb%26c%3Dd" {
		t.Fatalf("escapeS3Value = %q, want a%%2Bb%%26c%%3Dd", got)
	}
}
