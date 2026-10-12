package sqlite

import (
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// defaultBusyPragma 是 buildDSN 注入的默认 busy timeout（简记 P）。
const defaultBusyPragma = "_pragma=busy_timeout(5000)"

// testMemoryName 是 DSN 断言里固定使用的匿名内存库名（生产路径由 Open 生成）。
const testMemoryName = "mem_test"

func TestDialectorName(t *testing.T) {
	d := &dialector{}
	assert.Equal(t, "sqlite", d.Name())
}

func TestDialectorMatchURL(t *testing.T) {
	d := &dialector{}
	assert.True(t, d.MatchURL("sqlite://:memory:"))
	assert.True(t, d.MatchURL("sqlite:///path/to/db.sqlite"))
	// scheme 大小写不敏感
	assert.True(t, d.MatchURL("SQLITE://:memory:"))
	assert.True(t, d.MatchURL("Sqlite:///path/to/db.sqlite"))
	assert.True(t, d.MatchURL("  sqlite://db.sqlite"))
	assert.False(t, d.MatchURL("mysql://root:123456@127.0.0.1:3306/demo"))
	assert.False(t, d.MatchURL("postgres://user:pwd@127.0.0.1:5432/demo"))
	assert.False(t, d.MatchURL("sqlite:/path/to/db.sqlite"))
}

func TestDialectorParseURL(t *testing.T) {
	cases := []struct {
		name string
		url  string
		want string
	}{
		{"内存库", "sqlite://:memory:", ":memory:"},
		{"内存库 scheme 大写", "SQLITE://:memory:", ":memory:"},
		{"裸 scheme 与内存库一致", "sqlite://", ":memory:"},
		{"绝对路径", "sqlite:///path/to/db.sqlite", "/path/to/db.sqlite"},
		{"相对路径", "sqlite://./data/app.db", "./data/app.db"},
		{"带查询参数时只取路径", "sqlite:///path/to/db.sqlite?mode=ro", "/path/to/db.sqlite"},
		{"file: URI 去掉前缀", "sqlite://file:/abs/app.db?mode=ro", "/abs/app.db"},
		{"file: 内存库", "sqlite://file::memory:?cache=shared", ":memory:"},
		{"路径大小写保留", "SQLITE:///abs/APP.db", "/abs/APP.db"},
	}
	d := &dialector{}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := d.ParseURL(c.url)
			assert.NoError(t, err)
			assert.Equal(t, c.want, got)
		})
	}
}

// ParseURL 与 Dialector 必须对同一 URL 给出一致的语义，
// 否则日志里记录的库名和实际打开的库会对不上。
func TestParseURLMatchesDialector(t *testing.T) {
	d := &dialector{}
	for _, url := range []string{"sqlite://", "sqlite://:memory:", "SQLITE://:memory:"} {
		got, err := d.ParseURL(url)
		assert.NoError(t, err)
		assert.Equal(t, ":memory:", got, "url=%s", url)
		assert.Equal(t, "file:/"+testMemoryName+"?"+defaultBusyPragma+"&vfs=memdb",
			buildDSN(url, testMemoryName), "url=%s", url)
	}
}

func TestBuildDSN(t *testing.T) {
	cases := []struct {
		name string
		url  string
		want string
	}{
		{
			"内存库默认切到 memdb VFS",
			"sqlite://:memory:",
			"file:/mem_test?_pragma=busy_timeout(5000)&vfs=memdb",
		},
		{
			"裸 scheme 视为内存库",
			"sqlite://",
			"file:/mem_test?_pragma=busy_timeout(5000)&vfs=memdb",
		},
		{
			"mattn 风格的 file::memory:?cache=shared 仍等价改写",
			"sqlite://file::memory:?cache=shared",
			"file:/mem_test?_pragma=busy_timeout(5000)&cache=shared&vfs=memdb",
		},
		{
			"内存库显式 cache 不被覆盖",
			"sqlite://:memory:?cache=private",
			"file:/mem_test?_pragma=busy_timeout(5000)&cache=private&vfs=memdb",
		},
		{
			"命名内存库改写为 memdb 并剔除会破坏共享的 mode=memory",
			"sqlite://file:memdb1?mode=memory&cache=shared",
			"file:/memdb1?_pragma=busy_timeout(5000)&cache=shared&vfs=memdb",
		},
		{
			"已是 file:/ 形式且带 vfs 时不重复注入",
			"sqlite://file:/memdb2?mode=memory&vfs=other",
			"file:/memdb2?_pragma=busy_timeout(5000)&vfs=other",
		},
		{
			"文件库改写为 file: URI 并注入 _txlock",
			"sqlite:///abs/app.db",
			"file:/abs/app.db?_pragma=busy_timeout(5000)&_txlock=immediate",
		},
		{
			"相对路径",
			"sqlite://./data/app.db",
			"file:./data/app.db?_pragma=busy_timeout(5000)&_txlock=immediate",
		},
		{
			"mode=ro 等参数得以保留",
			"sqlite://app.db?mode=ro",
			"file:app.db?_pragma=busy_timeout(5000)&mode=ro&_txlock=immediate",
		},
		{
			"用户显式指定 _txlock 时不覆盖",
			"sqlite://app.db?_txlock=deferred",
			"file:app.db?_pragma=busy_timeout(5000)&_txlock=deferred",
		},
		{
			"用户显式指定 _txlock 且带其他参数",
			"sqlite://app.db?mode=ro&_txlock=exclusive",
			"file:app.db?_pragma=busy_timeout(5000)&mode=ro&_txlock=exclusive",
		},
		{
			"已是 file: URI 时保留原样",
			"sqlite://file:/abs/app.db?mode=ro",
			"file:/abs/app.db?_pragma=busy_timeout(5000)&mode=ro&_txlock=immediate",
		},
		{
			"已是 file: URI 且无参数",
			"sqlite://file:/abs/app.db",
			"file:/abs/app.db?_pragma=busy_timeout(5000)&_txlock=immediate",
		},
		{
			"路径中的百分号需转义，避免被 URI 解码",
			"sqlite:///tmp/%41/p.db",
			"file:/tmp/%2541/p.db?_pragma=busy_timeout(5000)&_txlock=immediate",
		},
		{
			"用户已设置 busy_timeout 时不再注入默认值",
			"sqlite://app.db?_pragma=busy_timeout(5000)",
			"file:app.db?_pragma=busy_timeout(5000)&_txlock=immediate",
		},
		{
			"其它 _pragma 不得抑制默认 busy_timeout，且默认值必须最前",
			"sqlite://app.db?_pragma=journal_mode(WAL)",
			"file:app.db?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_txlock=immediate",
		},
		{
			"mattn 的 _journal_mode 翻译为 _pragma",
			"sqlite://app.db?_journal_mode=WAL",
			"file:app.db?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_txlock=immediate",
		},
		{
			"翻译后的 _busy_timeout 抑制默认注入并排在最前",
			"sqlite://app.db?_busy_timeout=8000",
			"file:app.db?_pragma=busy_timeout(8000)&_txlock=immediate",
		},
		{
			"用户显式指定 _timefmt 时原样透传",
			"sqlite://app.db?_timefmt=rfc3339",
			"file:app.db?_pragma=busy_timeout(5000)&_timefmt=rfc3339&_txlock=immediate",
		},
		{
			"匹配用解码值，输出保留原文",
			"sqlite://app.db?_pragma=busy_timeout%3D3000",
			"file:app.db?_pragma=busy_timeout%3D3000&_txlock=immediate",
		},
		{
			"query 畸形时不注入，原样交给驱动报错",
			"sqlite://app.db?mode=%zz",
			"file:app.db?mode=%zz",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := buildDSN(c.url, testMemoryName)
			assert.Equal(t, c.want, got)
			// DSN 必须以 file: 开头，否则 _txlock/_timefmt/_pragma 会被驱动静默忽略。
			assert.True(t, strings.HasPrefix(got, "file:"), "DSN 必须以 file: 开头: %s", got)
		})
	}
}

// mattn 专有参数必须翻译成 _pragma，否则会被驱动静默忽略
// （dbaccess/README.md 推荐过 _journal_mode=WAL，它是真实的并发正确性参数）。
func TestMattnParamTranslation(t *testing.T) {
	got := buildDSN("sqlite://app.db?_journal_mode=WAL&_busy_timeout=8000", testMemoryName)
	assert.Equal(t, "file:app.db?_pragma=busy_timeout(8000)&_pragma=journal_mode(WAL)&_txlock=immediate", got)
	assert.NotContains(t, got, "busy_timeout(5000)", "翻译后的 busy_timeout 应抑制默认注入")
	assert.Equal(t, 1, strings.Count(got, "busy_timeout"), "busy_timeout 不应被重复注入")

	cases := []struct {
		url  string
		want string
	}{
		{"sqlite://app.db?_synchronous=FULL", "file:app.db?_pragma=busy_timeout(5000)&_pragma=synchronous(FULL)&_txlock=immediate"},
		{"sqlite://app.db?_foreign_keys=1", "file:app.db?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_txlock=immediate"},
		{"sqlite://app.db?_locking_mode=EXCLUSIVE", "file:app.db?_pragma=busy_timeout(5000)&_pragma=locking_mode(EXCLUSIVE)&_txlock=immediate"},
		// 未覆盖的 mattn 参数（_loc）无等价物，原样透传由驱动忽略。
		{"sqlite://app.db?_loc=auto", "file:app.db?_pragma=busy_timeout(5000)&_loc=auto&_txlock=immediate"},
	}
	for _, c := range cases {
		t.Run(c.url, func(t *testing.T) {
			assert.Equal(t, c.want, buildDSN(c.url, testMemoryName))
		})
	}
}

// Open 必须复用 buildDSN 这条归一化路径，而不是把 URL 直接交给驱动：
// 绕过归一化就丢掉了 busy_timeout 注入，测试会跑在与生产不同的 DSN 上。
func TestOpenUsesNormalizedDSN(t *testing.T) {
	cases := []struct {
		url     string
		wantDSN string
	}{
		{"sqlite://app.db", "file:app.db?_pragma=busy_timeout(5000)&_txlock=immediate"},
		{"SQLITE://app.db?mode=ro", "file:app.db?_pragma=busy_timeout(5000)&mode=ro&_txlock=immediate"},
		{"sqlite://file:memdb1?mode=memory", "file:/memdb1?_pragma=busy_timeout(5000)&vfs=memdb"},
	}
	for _, c := range cases {
		t.Run(c.url, func(t *testing.T) {
			d := Open(c.url)
			require.NotNil(t, d)
			assert.Equal(t, "sqlite", d.Name())
			assert.Equal(t, c.wantDSN, dialectorDSN(t, d))
		})
	}
}

// 匿名内存库的库名每次 Open 都不同：既要池内共享，又不能跨 Open 串库。
func TestOpenAnonymousMemoryIsolatedPerCall(t *testing.T) {
	d1 := Open("sqlite://:memory:")
	d2 := Open("sqlite://:memory:")
	dsn1, dsn2 := dialectorDSN(t, d1), dialectorDSN(t, d2)

	assert.NotEqual(t, dsn1, dsn2, "两次 Open 的匿名内存库必须相互隔离")
	for _, dsn := range []string{dsn1, dsn2} {
		assert.Regexp(t, `^file:/mem_\d+\?_pragma=busy_timeout\(5000\)&vfs=memdb$`, dsn)
	}
}

// dialectorDSN 从 gormlite 的方言句柄里取出归一化后的 DSN。
func dialectorDSN(t *testing.T, d gorm.Dialector) string {
	t.Helper()
	field := reflect.ValueOf(d).Elem().FieldByName("DSN")
	require.True(t, field.IsValid(), "gormlite 方言句柄不再导出 DSN 字段，需改用其它方式断言归一化")
	return field.String()
}
