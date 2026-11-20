package gormplugin

import "sync"

// sharedTableSet 是多个组件共享同一 *gorm.DB 时的**可变**白名单集合。
//
// 背景（为什么需要它）：EnableAppScope 用固定插件名调 db.Use()，gorm 对同名插件
// 返回 gorm.ErrRegistered。若把后一次调用的白名单当成"已注册、直接忽略"，则第二个
// 组件注册的表**根本不会被隔离**（fail-open）——这正是"以为隔离了其实没有"。因此
// 同一 db 上的多次 EnableAppScope 必须**合并**白名单，而不是丢弃后来者。
//
// 插件实例持有指向本集合的指针，白名单成长后已注册的回调读取到的是同一份最新集合，
// 无需重复 db.Use()。mutex 保护跨 goroutine 的读（查询回调并发执行）与写（注册期合并）。
type sharedTableSet struct {
	mu  sync.RWMutex
	set map[string]struct{}
}

func newSharedTableSet() *sharedTableSet {
	return &sharedTableSet{set: make(map[string]struct{})}
}

// add 合并规整后的表名（空串忽略），返回本集合。
func (s *sharedTableSet) add(tables ...string) *sharedTableSet {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range tables {
		if n := normalizeTableName(t); n != "" {
			s.set[n] = struct{}{}
		}
	}
	return s
}

// has 判断表名是否在白名单内（读取前规整）。空集合语义由调用方决定（见 inOnly）。
func (s *sharedTableSet) has(tableName string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.set[normalizeTableName(tableName)]
	return ok
}

// empty 报告集合是否为空（对应"未限制白名单"语义）。
func (s *sharedTableSet) empty() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.set) == 0
}

// appScopeRegistry 记录每个 *gorm.DB 上已注册的白名单集合，供重复 EnableAppScope
// 合并表名而非丢弃。用 *gorm.DB 指针作键：同一物理连接池复用同一实例，符合
// "共享同一 *gorm.DB"的语义。
var appScopeRegistry sync.Map // map[*gorm.DB]*sharedTableSet
