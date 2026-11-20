// Package gormdao 的租户/业务线隔离支持。
//
// 本文件提供"共享库多 app（业务线）隔离"的底座能力：给拥有 app_id 判别列的表，
// 在 GORM 回调层自动完成
//   - 查询/更新/删除：按 ctx 中的 app_id 自动追加 WHERE app_id = ?
//   - 创建：INSERT 前把 ctx 中的 app_id 写入实体
//
// 调用方无需在每个 SQL 上手工拼 app 条件，避免"漏写一处即跨 app 泄漏"。
//
// 设计取舍见 docs/notes/2026-11-20-terminal-multi-app-isolation-design.md：
//   - fail-closed：缺失 app 上下文时报 ErrAppMissing，而非退化为"全量"；
//   - 有 app 但查无记录仍返回 gorm.ErrRecordNotFound 原语义，不干扰 not-found 判断；
//   - 别名感知：Table("core_dict_item AS i") 时用别名 i 限定列，避免 JOIN 歧义；
//   - 单次注册：同一个 *gorm.DB 上只注册一次回调（幂等），避免多实例重复/覆盖。
package gormdao

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/schema"
)

// ErrAppMissing 在需要业务线隔离的组件表上、ctx 未携带 app 标识时返回。
var ErrAppMissing = errors.New("gormdao: app id missing in context")

// AppIDColumn 是各内置表约定的业务线判别列名。
const AppIDColumn = "app_id"

type appKey struct{}

// WithApp 把业务线标识放进 ctx，供下游 GORM 回调自动注入 app 条件。
// 约定由 HTTP/gRPC 中间件或异步任务入口调用。
func WithApp(ctx context.Context, appID string) context.Context {
	return context.WithValue(ctx, appKey{}, appID)
}

// AppFrom 从 ctx 取出业务线标识。ok 为 false 表示未设置或为空串。
func AppFrom(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(appKey{}).(string)
	return v, ok && v != ""
}

// TenantGetter 包装一个基础 *gorm.DB，返回带 ctx 的 DBGetter（与 base.DB 的 Session 语义一致）。
//
// base.DB 已经做过 Session 隔离，因此这里只需 WithContext 让 Statement.Context 携带 app，
// 不再重复 Session（见审查意见：双重 Session 冗余）。
func TenantGetter(base *gorm.DB) DBGetter {
	return func(ctx context.Context) *gorm.DB {
		return base.WithContext(ctx)
	}
}

// tenantRegistrar 记录已在某个 *gorm.DB 上注册过回调和其覆盖的表集合，
// 保证同一 db 只注册一次；后续以新增表调用时，把新表并入同一份配置。
type tenantRegistrar struct {
	mu     sync.Mutex
	tables map[string]struct{}
}

var (
	registered sync.Map // map[*gorm.DB]*tenantRegistrar
)

// RegisterTenantScope 在 db 上注册租户隔离回调，覆盖给定逻辑表名集合。
//
// 幂等：对同一个 *gorm.DB 多次调用只注册一次回调；后调用传入的新表会并入同一份配置，
// 因此推荐由装配层一次性传入全部组件表（如 dict/gsetting/... 的表名）。
//
// 注意：目前不解析 JOIN 中被关联的第二张表——需要跨表隔离的查询（如 dict 的 joinItems）
// 应改写为两次查询或显式在 JOIN 条件里加 app_id，勿依赖本回调。
func RegisterTenantScope(db *gorm.DB, tables ...string) error {
	if db == nil {
		return errors.New("gormdao: RegisterTenantScope requires a non-nil *gorm.DB")
	}

	regAny, loaded := registered.Load(db)
	if !loaded {
		reg := &tenantRegistrar{tables: make(map[string]struct{}, len(tables))}
		actual, ok := registered.LoadOrStore(db, reg)
		if ok {
			reg = actual.(*tenantRegistrar)
		} else {
			// 首次注册：挂上四个回调。
			if err := registerTenantCallbacks(db, reg); err != nil {
				registered.Delete(db)
				return err
			}
		}
		regAny = reg
	}

	reg := regAny.(*tenantRegistrar)
	reg.mu.Lock()
	for _, t := range tables {
		if t != "" {
			reg.tables[strings.ToLower(t)] = struct{}{}
		}
	}
	reg.mu.Unlock()
	return nil
}

func (r *tenantRegistrar) covers(table string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.tables[strings.ToLower(table)]
	return ok
}

func registerTenantCallbacks(db *gorm.DB, reg *tenantRegistrar) error {
	injectWhere := func(tx *gorm.DB) {
		_, alias, ok := resolveTable(tx, reg)
		if !ok {
			return // 非受控表，放行
		}
		app, has := AppFrom(tx.Statement.Context)
		if !has {
			_ = tx.AddError(ErrAppMissing) // fail-closed
			return
		}
		col := clause.Column{Name: AppIDColumn}
		if alias != "" {
			col.Table = alias // 别名限定，避免 JOIN 歧义
		}
		tx.Statement.AddClause(clause.Where{Exprs: []clause.Expression{
			clause.Eq{Column: col, Value: app},
		}})
	}

	injectCreate := func(tx *gorm.DB) {
		_, _, ok := resolveTable(tx, reg)
		if !ok {
			return
		}
		app, has := AppFrom(tx.Statement.Context)
		if !has {
			_ = tx.AddError(ErrAppMissing) // fail-closed：无 app 不写
			return
		}
		if err := setAppField(tx, AppIDColumn, app); err != nil {
			_ = tx.AddError(err)
		}
	}

	if err := db.Callback().Query().Before("gorm:query").Register("gorm:tenant_query", injectWhere); err != nil {
		return err
	}
	// Scan / Rows / Row 走 gorm:row 回调而非 gorm:query，需同样注入（否则 Scan 类查询绕过隔离）。
	if err := db.Callback().Row().Before("gorm:row").Register("gorm:tenant_row", injectWhere); err != nil {
		return err
	}
	if err := db.Callback().Update().Before("gorm:update").Register("gorm:tenant_update", injectWhere); err != nil {
		return err
	}
	if err := db.Callback().Delete().Before("gorm:delete").Register("gorm:tenant_delete", injectWhere); err != nil {
		return err
	}
	if err := db.Callback().Create().Before("gorm:create").Register("gorm:tenant_create", injectCreate); err != nil {
		return err
	}
	return nil
}

// resolveTable 根据 tx.Statement 解析出【逻辑表名】与【列限定用的别名】。
//
// 返回 ok=false 表示该表不受租户隔离管理（放行）。
//
// 关键：GORM 在 Table("tenant_row AS r") 时把 Statement.Table 设为**别名** "r"，
// 原始 "tenant_row AS r" 存在 Statement.TableExpr.SQL 里；因此先按 Table 直接匹配，
// 不中再从 TableExpr.SQL 解析真实表名与别名。覆盖形态：
//   - Statement.Table = "tenant_row"          → logical=tenant_row, alias=""
//   - Table="r" + TableExpr.SQL="tenant_row AS r" → logical=tenant_row, alias="r"
//   - 反引号/双引号包裹（`tenant_row` / "tenant_row"）→ 去引号后再比对
//   - 空（用 Model 推断）                       → 取 Statement.Schema.Table
func resolveTable(tx *gorm.DB, reg *tenantRegistrar) (logical string, alias string, ok bool) {
	// 1) 直接按 Statement.Table 匹配（无别名或 TableName 场景）
	if base, al, hit := matchTable(tx.Statement.Table, reg); hit {
		return base, al, true
	}
	// 2) 从 TableExpr.SQL 解析（Table("x AS y") 场景，此时 Statement.Table 只是别名 y）
	if tx.Statement.TableExpr != nil {
		if base, al, hit := matchTable(tx.Statement.TableExpr.SQL, reg); hit {
			if al == "" {
				al = unquoteIdent(tx.Statement.Table) // 别名来自 Statement.Table
			}
			return base, al, true
		}
	}
	// 3) Statement.Table 为空时用 Model 推断
	if tx.Statement.Table == "" && tx.Statement.Schema != nil {
		if base, al, hit := matchTable(tx.Statement.Schema.Table, reg); hit {
			return base, al, true
		}
	}
	return "", "", false
}

// matchTable 把候选表名（可能含别名/引号）解析后与注册集合比对。
func matchTable(candidate string, reg *tenantRegistrar) (logical string, alias string, ok bool) {
	if strings.TrimSpace(candidate) == "" {
		return "", "", false
	}
	base, al := splitAlias(candidate)
	base = unquoteIdent(base)
	if reg.covers(base) {
		return base, al, true
	}
	return "", "", false
}

// splitAlias 把 "core_dict_item AS i" / "core_dict_item i" 拆成 (core_dict_item, i)。
// 无别名时返回 (name, "")。
func splitAlias(name string) (string, string) {
	// 显式 AS（大小写不敏感，前后需空白）
	if i := indexKeywordCI(name, " as "); i >= 0 {
		return strings.TrimSpace(name[:i]), unquoteIdent(strings.TrimSpace(name[i+4:]))
	}
	// 隐式别名：空格分隔两段，且第二段不是表达式残留
	if fields := strings.Fields(name); len(fields) == 2 {
		return fields[0], unquoteIdent(fields[1])
	}
	return name, ""
}

// indexKeywordCI 在 s 中不区分大小写地查找第一个 kw 的下标，找不到返回 -1。
func indexKeywordCI(s, kw string) int {
	ls, lkw := strings.ToLower(s), strings.ToLower(kw)
	return strings.Index(ls, lkw)
}

// unquoteIdent 去掉标识符两侧的反引号或双引号（不处理 schema 限定名之外的复杂情况）。
func unquoteIdent(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '`' && s[len(s)-1] == '`') || (s[0] == '"' && s[len(s)-1] == '"') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// setAppField 把 app_id 写入 Statement.Dest（支持单条指针、值、切片/数组及其指针）。
//
// GORM 的 schema.Field.Set 只能作用于单条结构体，对切片会 panic；因此这里先按
// ReflectValue 的形态分流：切片/数组逐元素调用 Set，结构体直接 Set。
func setAppField(tx *gorm.DB, column, app string) error {
	f, hasField := (*schema.Field)(nil), false
	if tx.Statement.Schema != nil {
		if sf, ok := tx.Statement.Schema.FieldsByDBName[column]; ok && sf != nil {
			f, hasField = sf, true
		}
	}
	if !hasField {
		return setAppFieldReflect(tx.Statement.Dest, column, app)
	}

	rv := tx.Statement.ReflectValue
	for rv.Kind() == reflect.Ptr || rv.Kind() == reflect.Interface {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
	}
	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
		for i := 0; i < rv.Len(); i++ {
			if err := f.Set(tx.Statement.Context, rv.Index(i), app); err != nil {
				return err
			}
		}
		return nil
	case reflect.Struct:
		return f.Set(tx.Statement.Context, rv, app)
	default:
		return nil
	}
}

// setAppFieldReflect 兜底反射：按 gorm column 名或字段名匹配写入。
func setAppFieldReflect(dest any, column, app string) error {
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
			if err := setAppOnStruct(rv.Index(i), column, app); err != nil {
				return err
			}
		}
		return nil
	case reflect.Struct:
		return setAppOnStruct(rv, column, app)
	default:
		return nil
	}
}

func setAppOnStruct(rv reflect.Value, column, app string) error {
	for rv.Kind() == reflect.Ptr {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return nil
	}
	t := rv.Type()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := f.Tag.Get("gorm")
		if strings.Contains(tag, "column:"+column) || strings.EqualFold(f.Name, "AppID") {
			fv := rv.Field(i)
			if fv.CanSet() && fv.Kind() == reflect.String {
				fv.SetString(app)
				return nil
			}
		}
	}
	return nil
}
