package dbgorm

import (
	"fmt"
	"net/url"
	"strings"

	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type DatabaseType string

const (
	MySQL      DatabaseType = "mysql"
	PostgreSQL DatabaseType = "postgres"
)

type Dialect interface {
	Name() DatabaseType
	Dialector(url string) gorm.Dialector
	ParseURL(url string) (database string, err error)
}

func detectDialect(cfg *GormConfig) (Dialect, error) {
	return detectFromURL(cfg.URL)
}

func detectFromURL(urlStr string) (Dialect, error) {
	if urlStr == "" {
		return nil, fmt.Errorf("database url cannot be empty")
	}

	urlLower := strings.ToLower(urlStr)

	switch {
	case strings.HasPrefix(urlLower, "mysql://"):
		return &mysqlDialect{}, nil
	case strings.HasPrefix(urlLower, "postgres://"),
		strings.HasPrefix(urlLower, "postgresql://"):
		return &postgresDialect{}, nil
	default:
		return nil, fmt.Errorf("unsupported database url format, url must start with 'mysql://' or 'postgres://'%s", urlFormatDoc[1:])
	}
}

func normalizeMySQLURI(uri string) (string, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return "", fmt.Errorf("parse mysql uri failed: %w", err)
	}

	user := u.User.Username()
	pass, _ := u.User.Password()
	host := u.Hostname()
	port := u.Port()
	if port == "" {
		port = "3306"
	}
	db := strings.TrimPrefix(u.Path, "/")

	query := u.RawQuery
	if query != "" {
		query = "?" + query
	}

	return fmt.Sprintf("%s:%s@tcp(%s:%s)/%s%s", user, pass, host, port, db, query), nil
}

type mysqlDialect struct{}

func (d *mysqlDialect) Name() DatabaseType { return MySQL }
func (d *mysqlDialect) Dialector(urlStr string) gorm.Dialector {
	connStr, err := normalizeMySQLURI(urlStr)
	if err != nil {
		return mysql.Open(urlStr)
	}
	return mysql.Open(connStr)
}
func (d *mysqlDialect) ParseURL(urlStr string) (string, error) {
	u, err := url.Parse(urlStr)
	if err != nil {
		return "", fmt.Errorf("parse mysql uri failed: %w", err)
	}
	return strings.TrimPrefix(u.Path, "/"), nil
}

type postgresDialect struct{}

func (d *postgresDialect) Name() DatabaseType { return PostgreSQL }
func (d *postgresDialect) Dialector(urlStr string) gorm.Dialector {
	return postgres.Open(urlStr)
}
func (d *postgresDialect) ParseURL(urlStr string) (string, error) {
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
