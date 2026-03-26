package gormplugin

import (
	"context"
	"fmt"
	"strings"

	"github.com/morehao/golib/biz/gcontext"
	"github.com/morehao/golib/gutil"
	"gorm.io/gorm"
)

type Plugin struct {
	skipTablesMap  map[string]struct{}
	CompanyIDField string
}

func NewPlugin(skipTables ...string) *Plugin {
	m := make(map[string]struct{}, len(skipTables))
	for _, t := range skipTables {
		normalized := normalizeTableName(t)
		if normalized != "" {
			m[normalized] = struct{}{}
		}
	}
	return &Plugin{
		skipTablesMap:  m,
		CompanyIDField: "company_id",
	}
}

func (p *Plugin) Name() string { return "company_scope_plugin" }

func (p *Plugin) Initialize(db *gorm.DB) error {
	callbacks := []struct {
		name   string
		typ    string
		before string
		fn     func(*gorm.DB)
	}{
		{"company:query", "query", "gorm:query", p.addCompanyScope},
		{"company:update", "update", "gorm:update", p.addCompanyScope},
		{"company:delete", "delete", "gorm:delete", p.addCompanyScope},
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

func (p *Plugin) addCompanyScope(db *gorm.DB) {
	if db.Statement == nil || db.Statement.Context == nil {
		return
	}

	tableName := resolveTableName(db)
	if tableName == "" {
		return
	}

	if p.isSkipped(tableName) {
		return
	}

	companyID, ok := getCompanyID(db.Statement.Context)
	if !ok {
		return
	}

	db.Statement.Where(fmt.Sprintf("`%s`.%s = ?", tableName, p.CompanyIDField), companyID)
}

func (p *Plugin) isSkipped(tableName string) bool {
	normalized := normalizeTableName(tableName)
	if normalized == "" {
		return false
	}
	_, ok := p.skipTablesMap[normalized]
	return ok
}

func getCompanyID(ctx context.Context) (uint, bool) {
	if ctx == nil {
		return 0, false
	}

	v := ctx.Value(gcontext.KeyCompanyID)
	companyID := uint(gutil.VToInt64(v))
	return companyID, companyID > 0
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
