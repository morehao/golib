package dbgorm

import (
	"fmt"
	"sync"
	"time"

	"github.com/morehao/golib/glog"
	"gorm.io/gorm"
)

var registeredDialectorsMu sync.RWMutex
var registeredDialectors = make(map[string]DialectorFactory)

type DialectorFactory interface {
	Name() string
	MatchURL(url string) bool
	Dialector(url string) gorm.Dialector
	ParseURL(url string) (database string, err error)
}

func Register(name string, factory DialectorFactory) {
	registeredDialectorsMu.Lock()
	defer registeredDialectorsMu.Unlock()
	registeredDialectors[name] = factory
}

type Config struct {
	URL             string        `yaml:"url"`
	Service         string        `yaml:"service"`
	MaxSqlLen       int           `yaml:"max_sql_len"`
	SlowThreshold   time.Duration `yaml:"slow_threshold"`
	MaxIdleConns    int           `yaml:"max_idle_conns"`
	MaxOpenConns    int           `yaml:"max_open_conns"`
	ConnMaxLifetime time.Duration `yaml:"conn_max_lifetime"`
	callerSkip      int
	loggerConfig    *glog.LogConfig
}

type Option interface {
	apply(*Config)
}

type optionFunc func(*Config)

func (opt optionFunc) apply(cfg *Config) {
	opt(cfg)
}

func WithLogConfig(logConfig *glog.LogConfig) Option {
	return optionFunc(func(cfg *Config) {
		cfg.loggerConfig = logConfig
	})
}

func WithCallerSkip(skip int) Option {
	return optionFunc(func(cfg *Config) {
		cfg.callerSkip = skip
	})
}

func New(cfg *Config, opts ...Option) (*gorm.DB, error) {
	cfg.loggerConfig = glog.CloneLogConfig(glog.GetLoggerConfig())
	for _, opt := range opts {
		opt.apply(cfg)
	}

	factory, err := matchDialector(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("match dialector failed: %w", err)
	}

	database, parseErr := factory.ParseURL(cfg.URL)
	if parseErr != nil {
		return nil, fmt.Errorf("parse url failed: %w", parseErr)
	}

	service := cfg.Service
	if service == "" {
		service = database
	}

	customLogger, logErr := newOrmLogger(&ormConfig{
		Service:       service,
		Database:      database,
		MaxSqlLen:     cfg.MaxSqlLen,
		SlowThreshold: cfg.SlowThreshold,
		loggerConfig:  cfg.loggerConfig,
		callerSkip:    cfg.callerSkip,
	})
	if logErr != nil {
		return nil, fmt.Errorf("create logger failed: %w", logErr)
	}

	db, err := gorm.Open(factory.Dialector(cfg.URL), &gorm.Config{
		Logger: customLogger,
	})
	if err != nil {
		return nil, fmt.Errorf("open database failed: %w", err)
	}

	if cfg.MaxIdleConns > 0 || cfg.MaxOpenConns > 0 || cfg.ConnMaxLifetime > 0 {
		sqlDB, err := db.DB()
		if err != nil {
			return nil, fmt.Errorf("get sql.DB failed: %w", err)
		}

		if cfg.MaxIdleConns > 0 {
			sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
		}
		if cfg.MaxOpenConns > 0 {
			sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
		}
		if cfg.ConnMaxLifetime > 0 {
			sqlDB.SetConnMaxLifetime(cfg.ConnMaxLifetime)
		}
	}

	return db, nil
}

func matchDialector(url string) (DialectorFactory, error) {
	registeredDialectorsMu.RLock()
	defer registeredDialectorsMu.RUnlock()

	for _, factory := range registeredDialectors {
		if factory.MatchURL(url) {
			return factory, nil
		}
	}
	return nil, fmt.Errorf("no registered dialector matches url, make sure to import the driver (e.g. _ \"github.com/morehao/golib/dbaccess/dbgorm/driver/mysql\")")
}
