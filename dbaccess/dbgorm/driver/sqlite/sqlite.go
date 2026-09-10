// Package sqlite 为 dbgorm 提供 SQLite dialector。
//
// 支持的 URL 形式（scheme 大小写不敏感）：
//
//	sqlite://:memory:               内存库，连接池内共享同一个库
//	sqlite:///abs/path/app.db       绝对路径
//	sqlite://./data/app.db          相对路径
//	sqlite://data/app.db?mode=ro    带 SQLite URI 连接参数
//	sqlite://file:/abs/app.db?mode=ro   已是 file: URI 时原样保留
//
// 实现要点：
//
//   - 文件库统一改写为 file: URI 并转义路径，使 mode、cache、_pragma 等
//     连接参数真正生效；不这样做时 SQLite 会把参数静默丢弃。
//   - 文件库默认注入 _txlock=immediate，避免 BEGIN DEFERRED 在读写锁升级时
//     绕过 busy_timeout 直接报 "database is locked"；显式传入 _txlock 时不覆盖。
//   - 内存库改写为 file::memory:?cache=shared，否则连接池中每条连接都是一个
//     独立的空库，容易出现 "no such table"。
package sqlite

import (
	"strings"

	"github.com/morehao/golib/dbaccess/dbgorm"
	gormsqlite "gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

const (
	// scheme 前缀，匹配与剥离均忽略大小写。
	scheme = "sqlite://"
	// fileScheme 是 SQLite 的 URI 文件名前缀。
	fileScheme = "file:"
	// memoryDB 是 SQLite 的内存库标记。
	memoryDB = ":memory:"
	// defaultTxLock 让事务以 BEGIN IMMEDIATE 开始。
	defaultTxLock = "_txlock=immediate"
	// sharedMemoryBase 让连接池内的所有连接共享同一个内存库。
	sharedMemoryBase = "file::memory:"
)

type dialector struct{}

func init() {
	dbgorm.Register("sqlite", &dialector{})
}

func (d *dialector) Name() string {
	return "sqlite"
}

func (d *dialector) MatchURL(urlStr string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(urlStr)), scheme)
}

func (d *dialector) Dialector(urlStr string) gorm.Dialector {
	return gormsqlite.Open(buildDSN(urlStr))
}

func (d *dialector) ParseURL(urlStr string) (string, error) {
	path, _ := splitPathQuery(stripScheme(urlStr))
	path = stripFileScheme(path)
	if path == "" {
		return memoryDB, nil
	}
	return path, nil
}

// buildDSN 把 sqlite:// URL 归一化为 SQLite DSN。
//
// 路径与查询参数分开处理：file: URI 会按 URI 规则做百分号解码，因此路径中
// 的 %、?、# 必须转义，否则含这些字符的既有路径会被解析到别处。
func buildDSN(urlStr string) string {
	path, rawQuery := splitPathQuery(stripScheme(urlStr))

	// 空路径与 :memory: 等价，统一视为内存库。
	if path == "" || strings.EqualFold(path, memoryDB) {
		return memoryDSN(rawQuery)
	}

	// 已是 file: URI 时保留调用方的命名与转义，避免二次改写。
	if strings.HasPrefix(strings.ToLower(path), fileScheme) {
		return joinQuery(path, rawQuery)
	}

	return joinQuery(fileScheme+escapePath(path), rawQuery)
}

// memoryDSN 返回内存库 DSN，默认开启共享缓存。
func memoryDSN(rawQuery string) string {
	if rawQuery == "" {
		return sharedMemoryBase + "?cache=shared"
	}
	if !hasParam(rawQuery, "cache") {
		rawQuery += "&cache=shared"
	}
	return sharedMemoryBase + "?" + rawQuery
}

// joinQuery 追加默认事务模式，并在存在查询参数时拼接 SQLite URI。
func joinQuery(base, rawQuery string) string {
	txLock := defaultTxLock
	if hasParam(rawQuery, "_txlock") {
		return base + "?" + rawQuery
	}
	if rawQuery == "" {
		return base + "?" + txLock
	}
	return base + "?" + rawQuery + "&" + txLock
}

// stripScheme 忽略大小写地剥离 sqlite:// 前缀。
func stripScheme(urlStr string) string {
	trimmed := strings.TrimSpace(urlStr)
	if len(trimmed) >= len(scheme) && strings.EqualFold(trimmed[:len(scheme)], scheme) {
		return trimmed[len(scheme):]
	}
	return trimmed
}

// stripFileScheme 忽略大小写地剥离 file: 前缀。
func stripFileScheme(path string) string {
	if len(path) >= len(fileScheme) && strings.EqualFold(path[:len(fileScheme)], fileScheme) {
		return path[len(fileScheme):]
	}
	return path
}

// splitPathQuery 在第一个 ? 处切分路径与原始查询串。
func splitPathQuery(rest string) (path, rawQuery string) {
	if idx := strings.Index(rest, "?"); idx >= 0 {
		return rest[:idx], rest[idx+1:]
	}
	return rest, ""
}

// escapePath 转义会改变 file: URI 语义的字符。
func escapePath(path string) string {
	return strings.NewReplacer("%", "%25", "?", "%3f", "#", "%23").Replace(path)
}

// hasParam 判断原始查询串中是否已存在指定参数。
func hasParam(rawQuery, key string) bool {
	for _, kv := range strings.Split(rawQuery, "&") {
		if kv == "" {
			continue
		}
		k, _, _ := strings.Cut(kv, "=")
		if strings.EqualFold(strings.TrimSpace(k), key) {
			return true
		}
	}
	return false
}
