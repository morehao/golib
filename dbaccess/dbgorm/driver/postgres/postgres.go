package postgres

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/morehao/golib/dbaccess/dbgorm"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type dialector struct{}

func init() {
	dbgorm.Register("postgres", &dialector{})
}

func (d *dialector) Name() string {
	return "postgres"
}

func (d *dialector) MatchURL(urlStr string) bool {
	lower := strings.ToLower(urlStr)
	return strings.HasPrefix(lower, "postgres://") || strings.HasPrefix(lower, "postgresql://")
}

func (d *dialector) Dialector(urlStr string) gorm.Dialector {
	return postgres.Open(urlStr)
}

func (d *dialector) ParseURL(urlStr string) (string, error) {
	u, err := url.Parse(urlStr)
	if err != nil {
		return "", fmt.Errorf("parse postgres uri failed: %w", err)
	}
	db := strings.TrimPrefix(u.Path, "/")
	if db == "" {
		return "", fmt.Errorf("database name is required in postgres uri")
	}
	return db, nil
}
