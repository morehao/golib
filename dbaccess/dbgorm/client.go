package dbgorm

import (
	"fmt"

	"github.com/morehao/golib/glog"
	"gorm.io/gorm"
)

func New(cfg *GormConfig, opts ...Option) (*gorm.DB, error) {
	cfg.loggerConfig = glog.GetDefaultLogConfig()
	for _, opt := range opts {
		opt.apply(cfg)
	}

	dialect, err := detectDialect(cfg)
	if err != nil {
		return nil, fmt.Errorf("detect dialect failed: %w", err)
	}

	if cfg.Database == "" {
		database, parseErr := dialect.ParseDSN(cfg.DSN)
		if parseErr != nil {
			return nil, fmt.Errorf("parse dsn failed: %w", parseErr)
		}
		cfg.Database = database
	}

	service := cfg.Service
	if service == "" {
		service = cfg.Database
	}

	customLogger, logErr := newOrmLogger(&ormConfig{
		Service:       service,
		Database:      cfg.Database,
		MaxSqlLen:     cfg.MaxSqlLen,
		SlowThreshold: cfg.SlowThreshold,
		loggerConfig:  cfg.loggerConfig,
	})
	if logErr != nil {
		return nil, fmt.Errorf("create logger failed: %w", logErr)
	}

	db, err := gorm.Open(dialect.Dialector(cfg.DSN), &gorm.Config{
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

func WithLogConfig(logConfig *glog.LogConfig) Option {
	return optionFunc(func(cfg *GormConfig) {
		cfg.loggerConfig = logConfig
	})
}
