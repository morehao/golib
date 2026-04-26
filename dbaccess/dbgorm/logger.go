package dbgorm

import (
	"context"
	"errors"
	"time"

	"github.com/morehao/golib/glog"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
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
	glog.AppendExtraKeys(cfg.loggerConfig, glog.KeyAppRequestID)
	l, err := glog.NewLogger(cfg.loggerConfig, glog.WithCallerSkip(9))
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

func (l *ormLogger) Info(ctx context.Context, msg string, data ...any) {
	l.Logger.Infow(ctx, msg, append(l.commonFields(ctx), data...)...)
}

func (l *ormLogger) Warn(ctx context.Context, msg string, data ...any) {
	l.Logger.Warnw(ctx, msg, append(l.commonFields(ctx), data...)...)
}

func (l *ormLogger) Error(ctx context.Context, msg string, data ...any) {
	l.Logger.Errorw(ctx, msg, append(l.commonFields(ctx), data...)...)
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
		glog.KeyDbAffectedRows, rows,
		glog.KeyAppRequestDurationMs, cost,
		glog.KeyAppResponseCode, ralCode,
		glog.KeyDbStatement, sql,
	)

	if l.SlowThreshold > 0 && cost >= float64(l.SlowThreshold/time.Millisecond) {
		msg = "slow sql"
		l.Logger.Warnw(ctx, msg, fields...)
	} else {
		l.Logger.Debugw(ctx, msg, fields...)
	}
}

func (l *ormLogger) commonFields(ctx context.Context) []any {
	return []any{
		glog.KeyDbName, l.Database,
	}
}
