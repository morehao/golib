package dbgorm

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/morehao/golib/glog"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/utils"
)

type ormLogger struct {
	Service       string
	Database      string
	MaxSqlLen     int
	SlowThreshold time.Duration
	Logger        glog.Logger
}

type ormConfig struct {
	Service       string
	Database      string
	MaxSqlLen     int
	SlowThreshold time.Duration
	loggerConfig  *glog.LogConfig
}

func newOrmLogger(cfg *ormConfig) (*ormLogger, error) {
	s := cfg.Service
	if cfg.Service == "" {
		s = cfg.Database
	}
	l, err := glog.NewLogger(cfg.loggerConfig, glog.WithCallerSkip(5))
	if err != nil {
		return nil, err
	}
	return &ormLogger{
		Service:       s,
		Database:      cfg.Database,
		MaxSqlLen:     cfg.MaxSqlLen,
		SlowThreshold: cfg.SlowThreshold,
		Logger:        l,
	}, nil
}

func (l *ormLogger) LogMode(level logger.LogLevel) logger.Interface {
	return l
}

func (l *ormLogger) Info(ctx context.Context, msg string, data ...interface{}) {
	formatMsg := fmt.Sprintf(msg, append([]interface{}{utils.FileWithLineNum()}, data...)...)
	l.Logger.Infow(ctx, formatMsg, l.commonFields(ctx)...)
}

func (l *ormLogger) Warn(ctx context.Context, msg string, data ...interface{}) {
	formatMsg := fmt.Sprintf(msg, append([]interface{}{utils.FileWithLineNum()}, data...)...)
	l.Logger.Warnw(ctx, formatMsg, l.commonFields(ctx)...)
}

func (l *ormLogger) Error(ctx context.Context, msg string, data ...interface{}) {
	formatMsg := fmt.Sprintf(msg, append([]interface{}{utils.FileWithLineNum()}, data...)...)
	l.Logger.Errorw(ctx, formatMsg, l.commonFields(ctx)...)
}

func (l *ormLogger) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	end := time.Now()
	cost := glog.GetRequestCost(begin, end)

	msg := "sql execute success"
	var ralCode int
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		msg = err.Error()
		ralCode = -1
	}

	sql, rows := fc()
	if len(sql) > l.MaxSqlLen && l.MaxSqlLen > 0 {
		sql = sql[:l.MaxSqlLen]
	}

	fields := l.commonFields(ctx)
	fields = append(fields,
		glog.KeyAffectedRows, rows,
		glog.KeyCost, cost,
		glog.KeyRalCode, ralCode,
		glog.KeySql, sql,
	)

	if l.SlowThreshold > 0 && cost >= float64(l.SlowThreshold/time.Millisecond) {
		msg = "slow sql"
		l.Logger.Warnw(ctx, msg, fields...)
	} else {
		l.Logger.Debugw(ctx, msg, fields...)
	}
}

func (l *ormLogger) commonFields(ctx context.Context) []interface{} {
	return []interface{}{
		glog.KeyDatabase, l.Database,
	}
}
