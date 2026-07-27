package mysql

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/morehao/golib/dbaccess/dbgorm"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

type dialector struct{}

func init() {
	dbgorm.Register("mysql", &dialector{})
}

func (d *dialector) Name() string {
	return "mysql"
}

func (d *dialector) MatchURL(urlStr string) bool {
	return strings.HasPrefix(strings.ToLower(urlStr), "mysql://")
}

func (d *dialector) Dialector(urlStr string) gorm.Dialector {
	connStr, err := normalizeMySQLURI(urlStr)
	if err != nil {
		return mysql.Open(urlStr)
	}
	return mysql.Open(connStr)
}

func (d *dialector) ParseURL(urlStr string) (string, error) {
	u, err := url.Parse(urlStr)
	if err != nil {
		return "", fmt.Errorf("parse mysql uri failed: %w", err)
	}
	return strings.TrimPrefix(u.Path, "/"), nil
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
