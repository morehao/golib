package gormplugin

import (
	"context"
	"fmt"
	"strings"

	"gorm.io/gorm"
)

const SkipKey = "gorm:condition:skip"

type ScopePlugin struct {
	fieldName   string
	skipTables  map[string]struct{}
	extractFunc func(context.Context) (any, bool)
}

type Option func(*options)

type options struct {
	fieldName   string
	skipTables  map[string]struct{}
	extractFunc func(context.Context) (any, bool)
}

func WithField(name string) Option {
	return func(o *options) {
		o.fieldName = name
	}
}

func WithSkipTables(tables []string) Option {
	return func(o *options) {
		for _, t := range tables {
			normalized := normalizeTableName(t)
			if normalized != "" {
				o.skipTables[normalized] = struct{}{}
			}
		}
	}
}

func WithExtractFunc(fn func(context.Context) (any, bool)) Option {
	return func(o *options) {
		o.extractFunc = fn
	}
}

func New(opts ...Option) *ScopePlugin {
	o := &options{
		fieldName:  "tenant_id",
		skipTables: make(map[string]struct{}),
	}
	for _, opt := range opts {
		opt(o)
	}
	return &ScopePlugin{
		fieldName:   o.fieldName,
		skipTables:  o.skipTables,
		extractFunc: o.extractFunc,
	}
}

func (p *ScopePlugin) Name() string { return "scope_condition_plugin" }

func (p *ScopePlugin) Initialize(db *gorm.DB) error {
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

	if p.extractFunc == nil {
		return
	}

	value, ok := p.extractFunc(db.Statement.Context)
	if !ok {
		return
	}

	db.Statement.Where(fmt.Sprintf("`%s`.%s = ?", tableName, p.fieldName), value)
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
	tableName = strings.Trim(tableName, "`")
	if tableName == "" {
		return ""
	}

	fields := strings.Fields(tableName)
	if len(fields) == 0 {
		return ""
	}

	base := strings.Trim(fields[0], "`")
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
