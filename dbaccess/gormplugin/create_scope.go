package gormplugin

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"gorm.io/gorm"
)

// ErrCreateScopeMissing 在启用 CreateAppPlugin 且 ctx 未声明作用域时返回。
//
// 与 ScopePlugin 的 MissingScope 不同，CreateAppPlugin 覆盖的是 INSERT 路径：
// 归属字段必须在写入前确定，缺失作用域时拒绝写入（fail-closed），避免写入一条
// 归属不明的记录（后续任何按作用域的读取都查不到它，成为"孤儿数据"）。
var ErrCreateScopeMissing = errors.New("gormplugin: create scope missing in context")

// CreateScopeConfig 是创建 CreateAppPlugin 的配置，必填。
type CreateScopeConfig struct {
	// FieldName 指定创建时自动填充的归属字段名（如 app_id、tenant_id），必填。
	FieldName string
	// Resolver 解析本次写入应填充的作用域值，语义与 ScopeResolver 一致的三元组：
	//   - inject=true  ⇒ 把 value 写入实体字段（value 为空视为配置错误，返回错误）；
	//   - inject=false 且 declared=true ⇒ 显式全作用域（如跨租户种子写入），不填充；
	//   - declared=false ⇒ 缺少作用域，返回 ErrCreateScopeMissing（fail-closed）。
	//
	// 通常直接传 gcontext.TenantScopeFilter。缺省（nil）时退回 ExtractFunc。
	Resolver ScopeResolver
	// ExtractFunc 布尔版的取值入口，与 Resolver 二选一。
	ExtractFunc func(context.Context) (any, bool)
	// SkipTables 跳过自动填充的表（可选）。
	SkipTables []string
	// OnlyTables 仅对这些表自动填充（白名单，可选）；与 SkipTables 互斥。
	OnlyTables []string
	// sharedOnly 由 EnableAppScope 内部注入：与 ScopePlugin 共享同一份可变白名单。
	sharedOnly *sharedTableSet
}

// CreateAppPlugin 是"创建时自动填充归属字段"的 GORM 插件。
//
// 它是 ScopePlugin 的补充：ScopePlugin 覆盖 Query/Update/Delete 的条件注入，
// 但**有意不覆盖 INSERT**（认为归属字段应由实体显式赋值）。在"共享库 + 每个写入
// 自动归属当前作用域"的场景里，让调用方每处都手工赋值既啰嗦又易漏，因此单独提供
// 本插件补齐 Create 路径。两者可同时 db.Use。
type CreateAppPlugin struct {
	fieldName  string
	resolver   ScopeResolver
	skipTables map[string]struct{}
	onlyTables map[string]struct{}
	// sharedOnly 非空时优先于 onlyTables：与 ScopePlugin 共享同一份可变白名单，
	// 保证同一 db 上后注册的组件表也能被 Create 回调填充 app_id。
	sharedOnly *sharedTableSet
}

// NewCreateAppPlugin 创建 CreateAppPlugin。FieldName 必填；Resolver 与 ExtractFunc 二选一。
func NewCreateAppPlugin(cfg *CreateScopeConfig) (*CreateAppPlugin, error) {
	if cfg == nil {
		return nil, fmt.Errorf("gormplugin: CreateScopeConfig is required")
	}
	if strings.TrimSpace(cfg.FieldName) == "" {
		return nil, fmt.Errorf("gormplugin: CreateScopeConfig.FieldName is required")
	}
	if cfg.Resolver != nil && cfg.ExtractFunc != nil {
		return nil, fmt.Errorf("gormplugin: CreateScopeConfig.Resolver and ExtractFunc are mutually exclusive")
	}
	resolver := cfg.Resolver
	if resolver == nil {
		if cfg.ExtractFunc == nil {
			return nil, fmt.Errorf("gormplugin: CreateScopeConfig.Resolver or ExtractFunc is required")
		}
		extract := cfg.ExtractFunc
		resolver = func(ctx context.Context) (any, bool, bool) {
			v, ok := extract(ctx)
			return v, ok, ok
		}
	}
	if len(cfg.SkipTables) > 0 && len(cfg.OnlyTables) > 0 {
		return nil, fmt.Errorf("gormplugin: CreateScopeConfig.SkipTables and OnlyTables are mutually exclusive")
	}
	skip := make(map[string]struct{}, len(cfg.SkipTables))
	for _, t := range cfg.SkipTables {
		if n := normalizeTableName(t); n != "" {
			skip[n] = struct{}{}
		}
	}
	only := make(map[string]struct{}, len(cfg.OnlyTables))
	for _, t := range cfg.OnlyTables {
		if n := normalizeTableName(t); n != "" {
			only[n] = struct{}{}
		}
	}
	return &CreateAppPlugin{fieldName: cfg.FieldName, resolver: resolver, skipTables: skip, onlyTables: only, sharedOnly: cfg.sharedOnly}, nil
}

// Name 实现 gorm plugin.Plugin。
func (p *CreateAppPlugin) Name() string { return "create_scope_plugin" }

// Initialize 实现 gorm plugin.Plugin：在 gorm:create 之前注册填充回调。
func (p *CreateAppPlugin) Initialize(db *gorm.DB) error {
	return db.Callback().Create().Before("gorm:create").Register("gormplugin:create_scope", p.fill)
}

func (p *CreateAppPlugin) fill(db *gorm.DB) {
	if db.Statement == nil {
		return
	}
	if v, ok := db.Get(SkipKey); ok {
		if skip, ok := v.(bool); ok && skip {
			return
		}
	}
	table := resolveTableName(db)
	if table == "" || p.isSkipped(table) || !p.inOnly(table) {
		return
	}

	ctx := db.Statement.Context
	if ctx == nil {
		ctx = context.Background()
	}
	value, inject, declared := p.resolver(ctx)
	if !declared {
		db.AddError(fmt.Errorf("%w: table=%s, field=%s", ErrCreateScopeMissing, table, p.fieldName))
		return
	}
	if !inject {
		return // 显式全作用域：不填充
	}
	if isEmptyScopeValue(value) {
		db.AddError(fmt.Errorf("%w: table=%s, field=%s", ErrEmptyScopeValue, table, p.fieldName))
		return
	}
	if err := setField(db, p.fieldName, value); err != nil {
		db.AddError(err)
	}
}

func (p *CreateAppPlugin) isSkipped(table string) bool {
	_, ok := p.skipTables[normalizeTableName(table)]
	return ok
}

// inOnly 白名单语义：白名单为空返回 true（不限制）。
// 优先使用 sharedOnly（多组件共享同一 db 的合并白名单），否则用插件自身 onlyTables。
func (p *CreateAppPlugin) inOnly(table string) bool {
	if p.sharedOnly != nil {
		if p.sharedOnly.empty() {
			return true
		}
		return p.sharedOnly.has(table)
	}
	if len(p.onlyTables) == 0 {
		return true
	}
	_, ok := p.onlyTables[normalizeTableName(table)]
	return ok
}

// setField 把 value 写入 Statement.Dest（单条 / 切片 / 及其指针）的目标字段。
// 优先用 Schema 的 Field.Set（正确处理类型转换）；切片逐个 Set。
func setField(db *gorm.DB, column string, value any) error {
	stmt := db.Statement
	if stmt.Schema != nil {
		if f, ok := stmt.Schema.FieldsByDBName[column]; ok && f != nil {
			rv := stmt.ReflectValue
			for rv.Kind() == reflect.Ptr || rv.Kind() == reflect.Interface {
				if rv.IsNil() {
					return nil
				}
				rv = rv.Elem()
			}
			switch rv.Kind() {
			case reflect.Slice, reflect.Array:
				for i := 0; i < rv.Len(); i++ {
					if err := f.Set(db.Statement.Context, rv.Index(i), value); err != nil {
						return err
					}
				}
				return nil
			case reflect.Struct:
				return f.Set(db.Statement.Context, rv, value)
			}
			return nil
		}
	}
	return setFieldReflect(stmt.Dest, column, value)
}

func setFieldReflect(dest any, column string, value any) error {
	rv := reflect.ValueOf(dest)
	for rv.Kind() == reflect.Ptr || rv.Kind() == reflect.Interface {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
	}
	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
		for i := 0; i < rv.Len(); i++ {
			setFieldOnStruct(rv.Index(i), column, value)
		}
	case reflect.Struct:
		setFieldOnStruct(rv, column, value)
	}
	return nil
}

func setFieldOnStruct(rv reflect.Value, column string, value any) {
	for rv.Kind() == reflect.Ptr {
		if rv.IsNil() {
			return
		}
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return
	}
	t := rv.Type()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := f.Tag.Get("gorm")
		if !(strings.Contains(tag, "column:"+column) || strings.EqualFold(f.Name, camelize(column))) {
			continue
		}
		fv := rv.Field(i)
		if !fv.CanSet() {
			return
		}
		if s, ok := value.(string); ok && fv.Kind() == reflect.String {
			fv.SetString(s)
			return
		}
		vv := reflect.ValueOf(value)
		if vv.IsValid() && vv.Type().AssignableTo(fv.Type()) {
			fv.Set(vv)
		}
		return
	}
}

// camelize 把 snake_case 转 CamelCase（app_id -> AppID 只做近似，主要靠 gorm column tag 匹配）。
func camelize(s string) string {
	parts := strings.Split(s, "_")
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, "")
}
