package gormplugin

import (
	"context"
	"fmt"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const SkipKey = "gorm:condition:skip"

type ScopePlugin struct {
	fieldName   string
	skipTables  map[string]struct{}
	extractFunc func(context.Context) (any, bool)
}

// ScopeConfig 是创建 ScopePlugin 的配置，为必填入参。
// FieldName 与 ExtractFunc 为必填项，缺失时 New 返回 error。
type ScopeConfig struct {
	// FieldName 指定租户过滤字段名（如 tenant_id、company_id），必填。
	FieldName string
	// ExtractFunc 从 context 中提取租户过滤值及是否存在，必填。
	ExtractFunc func(context.Context) (any, bool)
	// SkipTables 指定跳过条件注入的表名列表，可选。
	SkipTables []string
}

// New 创建 ScopePlugin。FieldName 与 ExtractFunc 为必填配置，
// 缺失时返回 error，确保通用组件不隐含默认字段名。
func New(cfg *ScopeConfig) (*ScopePlugin, error) {
	if cfg == nil {
		return nil, fmt.Errorf("gormplugin: ScopeConfig is required")
	}
	if strings.TrimSpace(cfg.FieldName) == "" {
		return nil, fmt.Errorf("gormplugin: FieldName is required")
	}
	if cfg.ExtractFunc == nil {
		return nil, fmt.Errorf("gormplugin: ExtractFunc is required")
	}

	skipTables := make(map[string]struct{})
	for _, t := range cfg.SkipTables {
		normalized := normalizeTableName(t)
		if normalized != "" {
			skipTables[normalized] = struct{}{}
		}
	}

	return &ScopePlugin{
		fieldName:   cfg.FieldName,
		skipTables:  skipTables,
		extractFunc: cfg.ExtractFunc,
	}, nil
}

func (p *ScopePlugin) Name() string { return "scope_condition_plugin" }

func (p *ScopePlugin) Initialize(db *gorm.DB) error {
	if strings.TrimSpace(p.fieldName) == "" || p.extractFunc == nil {
		return fmt.Errorf("gormplugin: FieldName and ExtractFunc are required")
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

func (p *ScopePlugin) addScope(db *gorm.DB) {
	if db.Statement == nil || db.Statement.Context == nil {
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

	value, ok := p.extractFunc(db.Statement.Context)
	if !ok {
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

func (p *ScopePlugin) isSkipped(tableName string) bool {
	normalized := normalizeTableName(tableName)
	if normalized == "" {
		return false
	}
	_, ok := p.skipTables[normalized]
	return ok
}

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
