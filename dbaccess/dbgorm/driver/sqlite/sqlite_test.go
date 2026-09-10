package sqlite

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

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
		assert.Equal(t, "file::memory:?cache=shared", buildDSN(url), "url=%s", url)
	}
}

func TestBuildDSN(t *testing.T) {
	cases := []struct {
		name string
		url  string
		want string
	}{
		{
			"内存库默认共享缓存",
			"sqlite://:memory:",
			"file::memory:?cache=shared",
		},
		{
			"裸 scheme 视为内存库",
			"sqlite://",
			"file::memory:?cache=shared",
		},
		{
			"内存库显式 cache 不被覆盖",
			"sqlite://:memory:?cache=private",
			"file::memory:?cache=private",
		},
		{
			"文件库改写为 file: URI 并注入 _txlock",
			"sqlite:///abs/app.db",
			"file:/abs/app.db?_txlock=immediate",
		},
		{
			"相对路径",
			"sqlite://./data/app.db",
			"file:./data/app.db?_txlock=immediate",
		},
		{
			"mode=ro 等参数得以保留",
			"sqlite://app.db?mode=ro",
			"file:app.db?mode=ro&_txlock=immediate",
		},
		{
			"用户显式指定 _txlock 时不覆盖",
			"sqlite://app.db?_txlock=deferred",
			"file:app.db?_txlock=deferred",
		},
		{
			"用户显式指定 _txlock 且带其他参数",
			"sqlite://app.db?mode=ro&_txlock=exclusive",
			"file:app.db?mode=ro&_txlock=exclusive",
		},
		{
			"已是 file: URI 时保留原样",
			"sqlite://file:/abs/app.db?mode=ro",
			"file:/abs/app.db?mode=ro&_txlock=immediate",
		},
		{
			"已是 file: URI 且无参数",
			"sqlite://file:/abs/app.db",
			"file:/abs/app.db?_txlock=immediate",
		},
		{
			"路径中的百分号需转义，避免被 URI 解码",
			"sqlite:///tmp/%41/p.db",
			"file:/tmp/%2541/p.db?_txlock=immediate",
		},
		{
			"查询串中的百分号保持原样",
			"sqlite://app.db?_pragma=busy_timeout(5000)",
			"file:app.db?_pragma=busy_timeout(5000)&_txlock=immediate",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, buildDSN(c.url))
		})
	}
}
