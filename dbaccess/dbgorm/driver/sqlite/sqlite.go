package sqlite

import (
	"strings"

	"github.com/morehao/golib/dbaccess/dbgorm"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type dialector struct{}

func init() {
	dbgorm.Register("sqlite", &dialector{})
}

func (d *dialector) Name() string {
	return "sqlite"
}

func (d *dialector) MatchURL(urlStr string) bool {
	return strings.HasPrefix(strings.ToLower(urlStr), "sqlite://")
}

func (d *dialector) Dialector(urlStr string) gorm.Dialector {
	dbPath := strings.TrimPrefix(urlStr, "sqlite://")
	if dbPath == ":memory:" {
		return sqlite.Open(dbPath)
	}
	return sqlite.Open(dbPath)
}

func (d *dialector) ParseURL(urlStr string) (string, error) {
	dbName := strings.TrimPrefix(urlStr, "sqlite://")
	if dbName == "" || dbName == ":memory:" {
		return ":memory:", nil
	}
	return dbName, nil
}
