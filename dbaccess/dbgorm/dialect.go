package dbgorm

import (
	"fmt"
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
	Dialector(dsn string) gorm.Dialector
	ParseDSN(dsn string) (database string, err error)
}

func detectDialect(cfg *GormConfig) (Dialect, error) {
	if cfg.Driver != "" {
		return getDialect(DatabaseType(cfg.Driver))
	}
	return detectFromDSN(cfg.DSN)
}

func detectFromDSN(dsn string) (Dialect, error) {
	dsnLower := strings.ToLower(dsn)

	switch {
	case strings.HasPrefix(dsnLower, "postgres://"),
		strings.HasPrefix(dsnLower, "postgresql://"),
		strings.Contains(dsnLower, "port=") && strings.Contains(dsnLower, "host=") && strings.Contains(dsnLower, "user="):
		return &postgresDialect{}, nil

	case strings.Contains(dsn, "@tcp("),
		strings.Contains(dsn, ":@tcp("):
		return &mysqlDialect{}, nil

	default:
		return &mysqlDialect{}, nil
	}
}

func getDialect(dbType DatabaseType) (Dialect, error) {
	switch dbType {
	case MySQL:
		return &mysqlDialect{}, nil
	case PostgreSQL:
		return &postgresDialect{}, nil
	default:
		return nil, fmt.Errorf("unsupported database type: %s", dbType)
	}
}

type mysqlDialect struct{}

func (d *mysqlDialect) Name() DatabaseType { return MySQL }
func (d *mysqlDialect) Dialector(dsn string) gorm.Dialector {
	return mysql.Open(dsn)
}
func (d *mysqlDialect) ParseDSN(dsn string) (string, error) {
	parts := strings.Split(dsn, "/")
	if len(parts) < 2 {
		return "", fmt.Errorf("invalid mysql dsn format")
	}
	dbPart := strings.Split(parts[len(parts)-1], "?")[0]
	return dbPart, nil
}

type postgresDialect struct{}

func (d *postgresDialect) Name() DatabaseType { return PostgreSQL }
func (d *postgresDialect) Dialector(dsn string) gorm.Dialector {
	return postgres.Open(dsn)
}
func (d *postgresDialect) ParseDSN(dsn string) (string, error) {
	if strings.Contains(dsn, "dbname=") {
		parts := strings.Split(dsn, " ")
		for _, part := range parts {
			if strings.HasPrefix(part, "dbname=") {
				return strings.TrimPrefix(part, "dbname="), nil
			}
		}
	}
	if strings.Contains(dsn, "://") {
		parts := strings.Split(dsn, "/")
		if len(parts) >= 4 {
			dbPart := strings.Split(parts[3], "?")[0]
			return dbPart, nil
		}
	}
	return "", fmt.Errorf("invalid postgres dsn format")
}
