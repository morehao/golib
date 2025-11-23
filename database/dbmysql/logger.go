package dbmysql

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
	Addr          string
	Database      string
	MaxSqlLen     int
	SlowThreshold time.Duration
	Logger        glog.Logger
}

type ormConfig struct {
	Service      string
	Addr         string
	Database     string
	MaxSqlLen    int
	loggerConfig *glog.LogConfig
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
		Service:   s,
		Addr:      cfg.Addr,
		Database:  cfg.Database,
		MaxSqlLen: cfg.MaxSqlLen,
		Logger:    l,
	}, nil
}

// LogMode log mode
func (l *ormLogger) LogMode(level logger.LogLevel) logger.Interface {
	return l
}

// Info print info
func (l *ormLogger) Info(ctx context.Context, msg string, data ...interface{}) {
	formatMsg := fmt.Sprintf(msg, append([]interface{}{utils.FileWithLineNum()}, data...)...)
	l.Logger.Infow(ctx, formatMsg, l.commonFields(ctx)...)
}

// Warn print warn messages
func (l *ormLogger) Warn(ctx context.Context, msg string, data ...interface{}) {
	formatMsg := fmt.Sprintf(msg, append([]interface{}{utils.FileWithLineNum()}, data...)...)
	l.Logger.Warnw(ctx, formatMsg, l.commonFields(ctx)...)
}

// Error print error messages
func (l *ormLogger) Error(ctx context.Context, msg string, data ...interface{}) {
	formatMsg := fmt.Sprintf(msg, append([]interface{}{utils.FileWithLineNum()}, data...)...)
	l.Logger.Errorw(ctx, formatMsg, l.commonFields(ctx)...)
}

// Trace print sql message
func (l *ormLogger) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	end := time.Now()
	cost := glog.GetRequestCost(begin, end)

	msg := "sql execute success"
	var ralCode int
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		// 过滤未找到数据的错误
		msg = err.Error()
		ralCode = -1
	}
	sql, rows := fc()
	if len(sql) > l.MaxSqlLen && l.MaxSqlLen > 0 {
		sql = sql[:l.MaxSqlLen]
	}

	// fileLineNum := utils.FileWithLineNum()
	fields := l.commonFields(ctx)
	fields = append(fields,
		glog.KeyAffectedRows, rows,
		// glog.KeyFile, fileLineNum,
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
	fields := []interface{}{
		glog.KeyAddr, l.Addr,
		glog.KeyDatabase, l.Database,
	}
	return fields
}
