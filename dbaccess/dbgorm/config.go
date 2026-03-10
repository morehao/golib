package dbgorm

import (
	"time"

	"github.com/morehao/golib/glog"
)

type GormConfig struct {
	Driver          string        `yaml:"driver"`            // 数据库类型: mysql, postgres(可选,自动识别)
	DSN             string        `yaml:"dsn"`               // 数据源名称
	Service         string        `yaml:"service"`           // 服务名
	Database        string        `yaml:"database"`          // 数据库名(可选,从DSN解析)
	MaxSqlLen       int           `yaml:"max_sql_len"`       // 日志最大SQL长度
	SlowThreshold   time.Duration `yaml:"slow_threshold"`    // 慢SQL阈值
	MaxIdleConns    int           `yaml:"max_idle_conns"`    // 最大空闲连接数
	MaxOpenConns    int           `yaml:"max_open_conns"`    // 最大打开连接数
	ConnMaxLifetime time.Duration `yaml:"conn_max_lifetime"` // 连接最大存活时间
	loggerConfig    *glog.LogConfig
}

type Option interface {
	apply(*GormConfig)
}

type optionFunc func(*GormConfig)

func (opt optionFunc) apply(cfg *GormConfig) {
	opt(cfg)
}
