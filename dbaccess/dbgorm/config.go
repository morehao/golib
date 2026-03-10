package dbgorm

import (
	"time"

	"github.com/morehao/golib/glog"
)

type GormConfig struct {
	URL             string        `yaml:"url"`               // 数据库连接 URL
	Service         string        `yaml:"service"`           // 服务名(可选, 从 URL 解析数据库名作为默认值)
	MaxSqlLen       int           `yaml:"max_sql_len"`       // 日志最大SQL长度
	SlowThreshold   time.Duration `yaml:"slow_threshold"`    // 慢SQL阈值
	MaxIdleConns    int           `yaml:"max_idle_conns"`    // 最大空闲连接数
	MaxOpenConns    int           `yaml:"max_open_conns"`    // 最大打开连接数
	ConnMaxLifetime time.Duration `yaml:"conn_max_lifetime"` // 连接最大存活时间
	loggerConfig    *glog.LogConfig
}

const urlFormatDoc = `
支持的数据库连接 URL 格式 (必须使用 URI 格式):

MySQL:
  mysql://user:password@host:port/database?charset=utf8mb4&parseTime=True&loc=Local

PostgreSQL:
  postgres://user:password@host:port/database?sslmode=disable
  或
  postgresql://user:password@host:port/database?sslmode=disable
`

type Option interface {
	apply(*GormConfig)
}

type optionFunc func(*GormConfig)

func (opt optionFunc) apply(cfg *GormConfig) {
	opt(cfg)
}
