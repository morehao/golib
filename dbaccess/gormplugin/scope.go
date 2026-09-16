package gormplugin

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const SkipKey = "gorm:condition:skip"

// ErrEmptyScopeValue 表示 Resolver 声明了“需要注入”（inject=true）却没有给出可用值
// （nil 或空字符串）。这属于**配置实现错误**，而不是“缺少作用域”：
// 继续注入会生成永远不成立的条件（与 NULL 或空串比较），表现为“静默查不到数据”；
// 而按“不过滤”放行则会变成越权读。
// 因此这里一律拒绝本次 SQL，且不走 MissingScope 回调（应当修 Resolver，而不是调策略）。
var ErrEmptyScopeValue = errors.New("gormplugin: scope value is empty while inject is true")

// ScopeResolver 从 context 解析本次操作的作用域，返回三元组：
//   - value：需要注入的过滤值；
//   - inject：是否注入过滤条件（false 表示"已声明但不注入"，如全租户作用域）；
//   - declared：context 是否声明了作用域（false 触发 MissingScope 回调）。
//
// 与只返回 (value, ok) 的 ExtractFunc 相比，Resolver 能区分
// "没声明作用域"（可能是缺陷）与"声明了全租户作用域"（是显式决定）。
//
// 注意 inject 与 declared 同为 bool，位置写反不会编译报错，却会把"全租户放行"
// 与"缺少作用域"对调。实现时请复用唯一翻译点（如 gcontext.TenantScopeFilter），
// 不要在每个消费方各写一遍 switch。
type ScopeResolver func(ctx context.Context) (value any, inject bool, declared bool)

type ScopePlugin struct {
	fieldName    string
	skipTables   map[string]struct{}
	resolver     ScopeResolver
	missingScope func(db *gorm.DB, tableName string) error
}

// ScopeConfig 是创建 ScopePlugin 的配置，为必填入参。
// Resolver 与 ExtractFunc 二选一（同时提供会被 New 拒绝）；FieldName 必填。
type ScopeConfig struct {
	// FieldName 指定租户过滤字段名（如 tenant_id、company_id），必填。
	FieldName string
	// ExtractFunc 从 context 中提取租户过滤值及是否存在；返回值即"是否已声明作用域"。
	// 为兼容既有消费者保留；与 Resolver 二选一。
	ExtractFunc func(context.Context) (any, bool)
	// Resolver 解析作用域三元组，与 ExtractFunc 二选一。
	// 两者同时提供时 New 返回 error：安全配置的歧义应当响亮失败，而不是静默取其一。
	Resolver ScopeResolver
	// MissingScope 在 context 未声明作用域时回调：
	//   - 返回 error：本次 SQL 不执行（fail-closed，gorm 内置回调会因 db.Error 跳过）；
	//   - 返回 nil：按"不过滤"放行（灰度期只告警不拦截）。
	// 为 nil 时保持历史行为：静默不过滤。
	//
	// 回调只用于观测与决策，不应再改 Statement；此时 SQL 尚未构建
	// （Statement.SQL 为空），因此拿不到出错语句，只能记录表名与 ctx。
	MissingScope func(db *gorm.DB, tableName string) error
	// SkipTables 指定跳过条件注入的表名列表，可选。
	SkipTables []string
}

// New 创建 ScopePlugin。FieldName 与（Resolver 或 ExtractFunc）为必填配置，
// 缺失时返回 error，确保通用组件不隐含默认字段名。
func New(cfg *ScopeConfig) (*ScopePlugin, error) {
	if cfg == nil {
		return nil, fmt.Errorf("gormplugin: ScopeConfig is required")
	}
	if strings.TrimSpace(cfg.FieldName) == "" {
		return nil, fmt.Errorf("gormplugin: FieldName is required")
	}
	if cfg.Resolver != nil && cfg.ExtractFunc != nil {
		return nil, fmt.Errorf("gormplugin: ScopeConfig.Resolver and ExtractFunc are mutually exclusive")
	}

	resolver := cfg.Resolver
	if resolver == nil {
		if cfg.ExtractFunc == nil {
			return nil, fmt.Errorf("gormplugin: ScopeConfig.Resolver or ExtractFunc is required")
		}
		extract := cfg.ExtractFunc
		resolver = func(ctx context.Context) (any, bool, bool) {
			value, ok := extract(ctx)
			return value, ok, ok
		}
	}

	skipTables := make(map[string]struct{})
	for _, t := range cfg.SkipTables {
		normalized := normalizeTableName(t)
		if normalized != "" {
			skipTables[normalized] = struct{}{}
		}
	}

	return &ScopePlugin{
		fieldName:    cfg.FieldName,
		skipTables:   skipTables,
		resolver:     resolver,
		missingScope: cfg.MissingScope,
	}, nil
}

func (p *ScopePlugin) Name() string { return "scope_condition_plugin" }

func (p *ScopePlugin) Initialize(db *gorm.DB) error {
	if strings.TrimSpace(p.fieldName) == "" || p.resolver == nil {
		return fmt.Errorf("gormplugin: FieldName and Resolver are required")
	}
	callbacks := []struct {
		name   string
		typ    string
		before string
		fn     func(*gorm.DB)
	}{
		{"gormplugin:query", "query", "gorm:query", p.addScope},
		{"gormplugin:update", "update", "gorm:update", p.addScope},
		{"gormplugin:delete", "delete", "gorm:delete", p.addScope},
	}

	for _, cb := range callbacks {
		var registerErr error
		switch cb.typ {
		case "query":
			registerErr = db.Callback().Query().Before(cb.before).Register(cb.name, cb.fn)
		case "update":
			registerErr = db.Callback().Update().Before(cb.before).Register(cb.name, cb.fn)
		case "delete":
			registerErr = db.Callback().Delete().Before(cb.before).Register(cb.name, cb.fn)
		}
		if registerErr != nil {
			return fmt.Errorf("register %s callback: %w", cb.name, registerErr)
		}
	}
	return nil
}

// addScope 是注册在 query/update/delete 之前的作用域注入回调。
//
// 覆盖边界（有意为之，勿误以为"fail-closed 就写得进不去"）：
//   - 覆盖 SELECT/UPDATE/DELETE 的条件注入；
//   - **不覆盖 INSERT**：Create 不会注入，租户字段由实体显式赋值；
//   - **不覆盖原生 SQL**：db.Exec/db.Raw 不经过这三个回调；
//   - 命中 SkipTables 或调用过 Skip 时整体豁免（含 MissingScope 钩子）。
func (p *ScopePlugin) addScope(db *gorm.DB) {
	if db.Statement == nil {
		return
	}

	if v, ok := db.Get(SkipKey); ok {
		if skip, ok := v.(bool); ok && skip {
			return
		}
	}

	tableName := resolveTableName(db)
	if tableName == "" {
		return
	}

	if p.isSkipped(tableName) {
		return
	}

	// gorm 初始化 Statement 时会写入 context.Background()，此兜底用于防御手工构造的
	// Statement：没有 context 必须按"未声明作用域"处理，而不是静默跳过过滤。
	ctx := db.Statement.Context
	if ctx == nil {
		ctx = context.Background()
	}

	value, inject, declared := p.resolver(ctx)
	if !declared {
		// ctx 未声明作用域：默认保持历史行为（不注入、不报错）；
		// 配置了 MissingScope 时由调用方决定"只告警"还是 fail-closed。
		p.reportMissingScope(db, tableName)
		return
	}
	if !inject {
		return
	}
	if isEmptyScopeValue(value) {
		// 声明了注入却没有值：若照旧注入会得到永远不成立的 `= NULL` / `= ''`，
		// 若放行则变成无过滤。两种都不可接受，直接拒绝本次 SQL。
		db.AddError(fmt.Errorf("%w: table=%s, field=%s", ErrEmptyScopeValue, tableName, p.fieldName))
		return
	}

	// 使用 clause.Column 让 GORM 按方言引用标识符：
	// MySQL/SQLite 生成 `table`.`field`，PostgreSQL 生成 "table"."field"，
	// 避免硬编码反引号导致 PG 上每次查询都报语法错误。
	db.Statement.Where(
		gorm.Expr("?.? = ?",
			clause.Column{Name: tableName},
			clause.Column{Name: p.fieldName},
			value,
		),
	)
}

// reportMissingScope 把"未声明作用域"交给调用方处置：返回 error 即 fail-closed
// （本次 SQL 不执行），返回 nil 则按历史行为放行（灰度期只告警）。
func (p *ScopePlugin) reportMissingScope(db *gorm.DB, tableName string) {
	if p.missingScope == nil {
		return
	}
	if err := p.missingScope(db, tableName); err != nil {
		db.AddError(err)
	}
}

// isEmptyScopeValue 判定"声明要注入但值不可用"的实现级错误。
// 只覆盖 nil 与空字符串两种最常见形态；带类型的 nil（如 (*string)(nil)）仍会落到
// `= NULL`，需要调用方自行保证，不在本函数职责内。
func isEmptyScopeValue(value any) bool {
	if value == nil {
		return true
	}
	if s, ok := value.(string); ok {
		return s == ""
	}
	return false
}

func (p *ScopePlugin) isSkipped(tableName string) bool {
	normalized := normalizeTableName(tableName)
	if normalized == "" {
		return false
	}
	_, ok := p.skipTables[normalized]
	return ok
}

// Skip 让当前操作整体跳过作用域注入，同时也跳过 MissingScope 回调
// （即 fail-closed 也不再生效）。它是一次性的越权开关：优先用显式的
// "全租户作用域"（如 gcontext.AllScope()）表达跨租户意图，需要审计时更易追踪。
func Skip(db *gorm.DB) *gorm.DB {
	return db.Set(SkipKey, true)
}

func normalizeTableName(tableName string) string {
	tableName = strings.TrimSpace(tableName)
	// 兼容 MySQL/SQLite 反引号与 PostgreSQL 双引号两种标识符引用
	tableName = strings.Trim(tableName, "`\"")
	if tableName == "" {
		return ""
	}

	fields := strings.Fields(tableName)
	if len(fields) == 0 {
		return ""
	}

	base := strings.Trim(fields[0], "`\"")
	if idx := strings.LastIndex(base, "."); idx >= 0 {
		base = base[idx+1:]
	}
	return strings.ToLower(base)
}

// resolveTableName 获取当前操作的主表名
func resolveTableName(db *gorm.DB) string {
	if db.Statement.Table != "" {
		return db.Statement.Table
	}
	if db.Statement.Model != nil {
		stmt := &gorm.Statement{DB: db}
		if err := stmt.Parse(db.Statement.Model); err != nil {
			return ""
		}
		return stmt.Table
	}
	return ""
}
