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
// 引擎是纯 Go 实现（github.com/ncruces/go-sqlite3：SQLite 编译为 wasm 后翻译成
// Go），因此 CGO_ENABLED=0 也能构建与运行，不再需要 C 工具链。
//
// 实现要点：
//
//   - DSN 必须以 file: 开头，否则 _txlock/_timefmt/_pragma 会被驱动静默忽略
//     （ncruces driver.go:176-185）。非内存路径统一补 file: 前缀，内存库统一
//     改写为 file::memory:。
//   - 只要 query 里出现任意 _pragma，驱动就放弃自己 60s 的 busy timeout
//     （driver.go:184、:249）。因此必须自己注入 busy_timeout，否则并发写会
//     直接退化为 SQLITE_BUSY。
//   - busy_timeout 必须排在所有 PRAGMA 之前（driver.go:83-85），所以它是
//     前置插入，而不是"已存在 _pragma 就跳过"的短路。
//   - 文件库默认注入 _txlock=immediate，避免 BEGIN DEFERRED 在读写锁升级时
//     绕过 busy_timeout 直接报 "database is locked"；显式传入 _txlock 时不覆盖。
//   - 内存库统一改写为 file:/<库名>?vfs=memdb，否则连接池中每条连接都是一个
//     独立的空库，容易出现 "no such table"。驱动编译时带 SQLITE_OMIT_SHARED_CACHE
//     （官方推荐，见 https://sqlite.org/compile.html#omit_shared_cache），
//     cache=shared 在 ncruces 下是空操作；共享内存库要走官方 memdb VFS
//     （https://github.com/ncruces/go-sqlite3/discussions/97）。
//     匿名内存库（:memory:）的库名由每次 Open 生成，见 newMemoryName。
//   - 时间列用驱动默认格式（写入 time.RFC3339Nano、读取 Auto），本包不注入
//     _timefmt；调用方显式传入时原样透传。
package sqlite

import (
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/morehao/golib/dbaccess/dbgorm"
	"github.com/ncruces/go-sqlite3/gormlite"
	// 注册 memdb VFS：内存库靠它实现"连接池内共享同一个库"。
	_ "github.com/ncruces/go-sqlite3/vfs/memdb"
	"gorm.io/gorm"
)

const (
	// scheme 前缀，匹配与剥离均忽略大小写。
	scheme = "sqlite://"
	// fileScheme 是 SQLite 的 URI 文件名前缀，_txlock/_timefmt/_pragma 只在它之下生效。
	fileScheme = "file:"
	// memoryDB 是 SQLite 的内存库标记。
	memoryDB = ":memory:"
	// memoryNamePrefix 是匿名内存库在 memdb VFS 里的名字前缀。
	memoryNamePrefix = "mem_"

	// txLockKey 是事务锁模式的参数名。
	txLockKey = "_txlock"
	// pragmaKey 是 ncruces 执行 PRAGMA 的参数名，它的"值"是一条 PRAGMA 语句。
	pragmaKey = "_pragma"
	// cacheKey 是 SQLite 的缓存模式参数名；shared cache 已被驱动编译掉，
	// 该参数在内存库上只剩透传意义。
	cacheKey = "cache"
	// modeKey/memoryMode 用于识别 file:<name>?mode=memory 形式的命名内存库。
	modeKey    = "mode"
	memoryMode = "memory"
	// vfsKey/vfsMemdb 把内存库切到 ncruces 的 memdb VFS。
	vfsKey   = "vfs"
	vfsMemdb = "memdb"

	// defaultTxLock 让事务以 BEGIN IMMEDIATE 开始。
	defaultTxLock = "immediate"

	// busyTimeout 对齐 mattn/go-sqlite3 隐式的 5000ms busy timeout，前置注入。
	busyTimeout = "busy_timeout(5000)"
)

// memorySeq 给每次 Open 的匿名内存库生成不同的库名。
var memorySeq atomic.Uint64

// newMemoryName 返回下一个匿名内存库名。
//
// 每次 Open 用独立库名要同时满足两件事：
//   - 同一个连接池内的连接共享同一个库，否则会间歇性报 "no such table"；
//   - 不同 Open 之间互不串库。:memory: 的语义就是"每次打开一个空库"，
//     固定库名会让后开的库看到先前测试遗留的表和数据（实测会让
//     task/gasync、task/gcron、configkv 的迁移用例相互污染）。
func newMemoryName() string {
	return memoryNamePrefix + strconv.FormatUint(memorySeq.Add(1), 10)
}

// mattnParams 把 mattn/go-sqlite3 专有的 URI 参数翻译为 ncruces 的 _pragma 语句。
// 未列出的 mattn 参数（如 _loc）在 ncruces 下没有等价物，会被忽略，
// 对照表见 dbaccess/README.md。
var mattnParams = map[string]string{
	"_busy_timeout": "busy_timeout",
	"_journal_mode": "journal_mode",
	"_synchronous":  "synchronous",
	"_foreign_keys": "foreign_keys",
	"_locking_mode": "locking_mode",
}

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

// Open 用 dbgorm 约定的 sqlite:// URL 构造方言。
//
// 导出它是为了让测试与高级用法复用同一条归一化路径：直接调 gormlite.Open
// 会绕过 busy_timeout 注入等行为，测试也就跑在与生产不同的 DSN 上。
//
// 每次调用都会为匿名内存库分配一个新库名（见 newMemoryName），所以两次 Open
// 拿到的是两个互不可见的空库。
func Open(urlStr string) gorm.Dialector {
	return gormlite.Open(buildDSN(urlStr, newMemoryName()))
}

func (d *dialector) Dialector(urlStr string) gorm.Dialector {
	return Open(urlStr)
}

func (d *dialector) ParseURL(urlStr string) (string, error) {
	path, _ := splitPathQuery(stripScheme(urlStr))
	path = stripFileScheme(path)
	if path == "" {
		return memoryDB, nil
	}
	return path, nil
}

// buildDSN 把 sqlite:// URL 归一化为驱动 DSN。
//
// 路径与查询参数分开处理：file: URI 会按 URI 规则做百分号解码，因此路径中
// 的 %、?、# 必须转义，否则含这些字符的既有路径会被解析到别处。
// 调用方提供的查询参数原样透传（便于人工粘进 CLI 排障），只有本包注入的
// 默认值才做转义。
//
// memoryName 是匿名内存库（:memory:）在 memdb VFS 中的库名；生产路径由
// Open 用 newMemoryName 生成，保证不同 Open 之间互不可见。
func buildDSN(urlStr, memoryName string) string {
	path, rawQuery := splitPathQuery(stripScheme(urlStr))

	params, ok := parseQuery(rawQuery)
	if !ok {
		// query 畸形：不注入、原样交给驱动，由 gorm.Open 报错。
		// Dialector(url) 没有 error 返回值，只能这样处理；但 file: 前缀仍要补，
		// 否则内存库连开都开不起来。
		switch {
		case isAnonymousMemory(path):
			path = fileScheme + "/" + memoryName
		case !strings.HasPrefix(strings.ToLower(path), fileScheme):
			path = fileScheme + escapePath(path)
		}
		return path + "?" + rawQuery
	}

	memory := false
	switch {
	case isAnonymousMemory(path):
		memory, path = true, fileScheme+"/"+memoryName
	default:
		if name, named := namedMemoryDB(path, params); named {
			// 命名内存库：不切 memdb VFS 就会退化成"每条连接一个私有空库"，
			// 且必须剔除 mode=memory——实测它会覆盖 memdb 的共享语义。
			// 库名沿用调用方给的名字，同名 URI 之间因此仍然共享。
			memory, path = true, fileScheme+"/"+name
			params = dropKeys(params, modeKey)
		} else if !strings.HasPrefix(strings.ToLower(path), fileScheme) {
			// file: 前缀是 _txlock/_timefmt/_pragma 生效的硬前提。
			path = fileScheme + escapePath(path)
		}
	}

	params = translateMattnParams(params)
	// 保证 busy_timeout 排在所有 PRAGMA 之前，见 hoistBusyTimeout 的说明。
	params = hoistBusyTimeout(params)

	out := make([]dsnParam, 0, len(params)+2)
	// 只在调用方未自行设置 busy_timeout 时注入默认值。
	if !hasBusyTimeout(params) {
		out = append(out, dsnParam{key: pragmaKey, val: busyTimeout})
	}
	out = append(out, params...)

	if memory {
		if !hasKey(out, vfsKey) {
			out = append(out, dsnParam{key: vfsKey, val: vfsMemdb})
		}
	} else if !hasKey(out, txLockKey) {
		out = append(out, dsnParam{key: txLockKey, val: defaultTxLock})
	}
	return joinParams(path, out)
}

// isAnonymousMemory 判断 :memory: 的三种写法：空路径、:memory:、file::memory:。
func isAnonymousMemory(path string) bool {
	return path == "" || strings.EqualFold(path, memoryDB) ||
		strings.EqualFold(stripFileScheme(path), memoryDB)
}

// namedMemoryDB 判断 file:<name>?mode=memory 形式的命名内存库，返回其库名。
//
// mattn 时期这种写法靠 cache=shared 在连接间共享，而 ncruces 编译时带了
// SQLITE_OMIT_SHARED_CACHE，cache=shared 是空操作：不改写就等于把共享内存库
// 静默降级成"每条连接一个私有空库"。
func namedMemoryDB(path string, params []dsnParam) (string, bool) {
	if !strings.HasPrefix(strings.ToLower(path), fileScheme) {
		return "", false
	}
	name := stripFileScheme(path)
	if name == "" {
		return "", false
	}
	for _, p := range params {
		if strings.EqualFold(strings.TrimSpace(unescape(p.key)), modeKey) &&
			strings.EqualFold(strings.TrimSpace(unescape(p.val)), memoryMode) {
			return strings.TrimPrefix(name, "/"), true
		}
	}
	return "", false
}

// dropKeys 剔除指定键的参数（按解码后的键比较，忽略大小写）。
func dropKeys(params []dsnParam, keys ...string) []dsnParam {
	out := params[:0]
	for _, p := range params {
		drop := false
		for _, k := range keys {
			if strings.EqualFold(strings.TrimSpace(unescape(p.key)), k) {
				drop = true
				break
			}
		}
		if !drop {
			out = append(out, p)
		}
	}
	return out
}

// dsnParam 是 DSN 查询串里的一组键值。
//
// literal 为 true 表示键值来自调用方的原始 query，输出必须逐字节保留
// （例如 _pragma=busy_timeout%3D3000 不能被二次转义）；零值表示由本包注入，
// 输出前需要做最小转义。
type dsnParam struct {
	key, val string
	literal  bool
}

// parseQuery 按 & 切分原始查询串并保留原文（输出用原文、匹配用解码值）。
// 返回 false 表示 query 含非法的百分号转义，调用方应原样透传给驱动报错。
func parseQuery(rawQuery string) ([]dsnParam, bool) {
	if rawQuery == "" {
		return nil, true
	}
	parts := strings.Split(rawQuery, "&")
	params := make([]dsnParam, 0, len(parts))
	for _, kv := range parts {
		if kv == "" {
			continue
		}
		if _, err := url.QueryUnescape(kv); err != nil {
			return nil, false
		}
		key, val, _ := strings.Cut(kv, "=")
		params = append(params, dsnParam{key: key, val: val, literal: true})
	}
	return params, true
}

// translateMattnParams 把 mattn 专有参数就地改写为 _pragma 语句，位置保持不变。
func translateMattnParams(params []dsnParam) []dsnParam {
	for i, p := range params {
		name, ok := mattnParams[strings.ToLower(strings.TrimSpace(unescape(p.key)))]
		if !ok {
			continue
		}
		// 值由本包重新拼接，不是原始 query 片段，因此需要转义。
		params[i] = dsnParam{key: pragmaKey, val: name + "(" + unescape(p.val) + ")"}
	}
	return params
}

// hasKey 判断参数中是否已存在指定键（按解码后的键比较，忽略大小写）。
func hasKey(params []dsnParam, key string) bool {
	for _, p := range params {
		if strings.EqualFold(strings.TrimSpace(unescape(p.key)), key) {
			return true
		}
	}
	return false
}

// hasBusyTimeout 判断调用方是否已自行设置 busy_timeout。
func hasBusyTimeout(params []dsnParam) bool {
	return busyTimeoutIndex(params) >= 0
}

// hoistBusyTimeout 把调用方设置的 busy_timeout pragma 移到最前。
//
// 驱动把所有 _pragma 按 query 顺序拼成一段 PRAGMA 脚本一次执行
// （conn.go:124-131），而 busy timeout 应当先于其它 PRAGMA 生效
// （driver.go:83-85 "busy timeout and locking mode should be the first
// PRAGMAs set"），所以这里不能只做就地翻译。
func hoistBusyTimeout(params []dsnParam) []dsnParam {
	idx := busyTimeoutIndex(params)
	if idx <= 0 {
		return params
	}
	out := make([]dsnParam, 0, len(params))
	out = append(out, params[idx])
	out = append(out, params[:idx]...)
	out = append(out, params[idx+1:]...)
	return out
}

// busyTimeoutIndex 返回第一个 busy_timeout pragma 的下标，不存在时返回 -1。
func busyTimeoutIndex(params []dsnParam) int {
	for i, p := range params {
		if isBusyTimeout(p) {
			return i
		}
	}
	return -1
}

// isBusyTimeout 判断单个参数是不是设置 busy_timeout 的 _pragma。
//
// 不能用 hasKey(params, "_pragma") 代替：_pragma 的"键"是一条 PRAGMA 语句而
// 不是参数名，必须解析值；兼容 busy_timeout(5000)、busy_timeout=5000、
// busy_timeout 5000 三种写法，且忽略大小写。
func isBusyTimeout(p dsnParam) bool {
	if !strings.EqualFold(strings.TrimSpace(unescape(p.key)), pragmaKey) {
		return false
	}
	name := strings.TrimSpace(unescape(p.val))
	if i := strings.IndexAny(name, "(= \t"); i >= 0 {
		name = name[:i]
	}
	return strings.EqualFold(strings.TrimSpace(name), "busy_timeout")
}

// joinParams 用 & 拼接路径与查询参数，只对注入值做最小转义。
func joinParams(path string, params []dsnParam) string {
	if len(params) == 0 {
		return path
	}
	parts := make([]string, 0, len(params))
	for _, p := range params {
		val := p.val
		if !p.literal {
			val = escapeValue(val)
		}
		parts = append(parts, p.key+"="+val)
	}
	return path + "?" + strings.Join(parts, "&")
}

// escapeValue 转义注入值中会截断查询串或改变语义的字符。
// 驱动用 url.ParseQuery 解码（%20 与 + 都会被还原成空格）。
func escapeValue(val string) string {
	return strings.NewReplacer(
		"%", "%25",
		"&", "%26",
		"+", "%2b",
		"#", "%23",
		" ", "%20",
	).Replace(val)
}

// unescape 返回键值的解码形式，仅供匹配使用；输出始终使用原文。
func unescape(s string) string {
	if decoded, err := url.QueryUnescape(s); err == nil {
		return decoded
	}
	return s
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
