package ddlgen

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// section 是 README 里一个 ```sql 代码块对应的「组件 + 方言」，顺序必须与 README 中代码块出现顺序一致。
type section struct {
	group   string
	dialect Dialect
}

// readmeCase 一个 README 及其应有的 sql 代码块。
//
// 这些顺序是硬约定：README 的「附录：手工建表 DDL」章节按此顺序给出各包 DDL。
// 想调整顺序就同时改这里，测试会保证两边不错位。
var readmeCases = []struct {
	readme   string
	sections []section
}{
	{"dict/README.md", []section{{"dict", MySQL}, {"dict", Postgres}}},
	{"configkv/README.md", []section{{"configkv", MySQL}, {"configkv", Postgres}}},
	{"filestore/README.md", []section{{"filestore", MySQL}, {"filestore", Postgres}}},
	{"task/README.md", []section{
		{"gcron", MySQL}, {"gcron", Postgres},
		{"gasync", MySQL}, {"gasync", Postgres},
	}},
}

// TestReadmeDDLMatchesModels 保证 README 里的 DDL 与各包 gorm tag 不漂移。
//
// 这是 `dict/schema_mysql.sql` / `schema_postgres.sql` 被删掉后留下的替代物：
// DDL 不再是仓库里的第二个手写事实源，而是由 model.go 生成、并在测试中被钉住。
func TestReadmeDDLMatchesModels(t *testing.T) {
	for _, tc := range readmeCases {
		path := filepath.Join("..", "..", filepath.FromSlash(tc.readme))
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("读取 %s 失败: %v", tc.readme, err)
		}
		blocks := ExtractSQLBlocks(string(raw))
		if len(blocks) != len(tc.sections) {
			t.Errorf("%s: 有 %d 个 ```sql 代码块，期望 %d 个（每个「组件 + 方言」一个）",
				tc.readme, len(blocks), len(tc.sections))
			continue
		}
		for i, sec := range tc.sections {
			group, ok := Lookup(sec.group)
			if !ok {
				t.Fatalf("未登记的组件 %q（可选项：%s）", sec.group, strings.Join(GroupNames(), ", "))
			}
			stmts, err := Generate(sec.dialect, group.Models)
			if err != nil {
				t.Fatalf("生成 %s/%s DDL 失败: %v", sec.group, sec.dialect, err)
			}
			want := normalize(strings.Join(stmts, "\n\n"))
			got := normalize(blocks[i])
			if want == got {
				continue
			}
			t.Errorf(`%s 第 %d 个 sql 代码块（%s / %s）与 %s 的 gorm tag 不一致。
请执行 `+"`go run internal/ddlgen/main.go %s`"+` 重新生成并同步该代码块。
%s`, tc.readme, i+1, sec.group, sec.dialect, sec.group, sec.group, firstDiff(want, got))
		}
	}
}

// TestGenerateCoversSchemaModels 保证生成器对每个组件都真的产出了建表语句，
// 避免出现"模型清单写错导致生成空 DDL、README 也跟着空"的假通过。
func TestGenerateCoversSchemaModels(t *testing.T) {
	for _, group := range Groups {
		if len(group.Models) == 0 {
			t.Errorf("%s: SchemaModels() 为空", group.Name)
			continue
		}
		for _, dialect := range Dialects {
			stmts, err := Generate(dialect, group.Models)
			if err != nil {
				t.Fatalf("%s/%s: %v", group.Name, dialect, err)
			}
			tables := 0
			for _, stmt := range stmts {
				if strings.HasPrefix(stmt, "CREATE TABLE ") {
					tables++
				}
			}
			if tables != len(group.Models) {
				t.Errorf("%s/%s: 有 %d 个模型，却生成 %d 条 CREATE TABLE",
					group.Name, dialect, len(group.Models), tables)
			}
		}
	}
}

// ExtractSQLBlocks 抽取 markdown 中全部 ```sql 代码块的原始内容，保持出现顺序。
func ExtractSQLBlocks(markdown string) []string {
	const fence = "```sql\n"
	var blocks []string
	for {
		start := strings.Index(markdown, fence)
		if start < 0 {
			return blocks
		}
		markdown = markdown[start+len(fence):]
		end := strings.Index(markdown, "```")
		if end < 0 {
			return blocks
		}
		blocks = append(blocks, markdown[:end])
		markdown = markdown[end:]
	}
}

// normalize 只对比语义，忽略空白与换行差异。
func normalize(s string) string { return strings.Join(strings.Fields(s), " ") }

// firstDiff 返回第一处差异的上下文，便于定位。
func firstDiff(want, got string) string {
	for i := 0; i < len(want) && i < len(got); i++ {
		if want[i] != got[i] {
			lo := max(0, i-80)
			return "第一处差异 @" + strconv.Itoa(i) + ":\n  want: ..." + want[lo:min(len(want), i+80)] +
				"\n  got:  ..." + got[lo:min(len(got), i+80)]
		}
	}
	return "长度不同: want=" + strconv.Itoa(len(want)) + " got=" + strconv.Itoa(len(got))
}
