package dbgorm

import (
	"testing"
	"time"

	"github.com/morehao/golib/glog"
	"github.com/stretchr/testify/assert"
)

func TestNew(t *testing.T) {
	defer glog.Close()
	logCfg := &glog.LogConfig{
		Service:   "app",
		Level:     glog.DebugLevel,
		Writer:    glog.WriterConsole,
		ExtraKeys: []string{glog.KeyRequestId},
	}
	initLogErr := glog.InitLogger(logCfg, glog.WithCallerSkip(1))
	assert.Nil(t, initLogErr)

	cfg := &GormConfig{
		URL:             "mysql://root:123456@127.0.0.1:3306/demo?charset=utf8mb4&parseTime=True&loc=Local",
		Service:         "test-service",
		MaxSqlLen:       1000,
		SlowThreshold:   time.Second,
		MaxIdleConns:    10,
		MaxOpenConns:    100,
		ConnMaxLifetime: time.Hour,
	}
	db, err := New(cfg)
	assert.Nil(t, err)
	assert.NotNil(t, db)

	var result int
	err = db.Raw("SELECT 1").Scan(&result).Error
	assert.Nil(t, err)
	assert.Equal(t, 1, result)

	sqlDB, err := db.DB()
	assert.Nil(t, err)
	assert.NotNil(t, sqlDB)
	err = sqlDB.Close()
	assert.Nil(t, err)
}

func TestNewWithoutService(t *testing.T) {
	defer glog.Close()
	logCfg := &glog.LogConfig{
		Service:   "app",
		Level:     glog.DebugLevel,
		Writer:    glog.WriterConsole,
		ExtraKeys: []string{glog.KeyRequestId},
	}
	initLogErr := glog.InitLogger(logCfg, glog.WithCallerSkip(1))
	assert.Nil(t, initLogErr)

	cfg := &GormConfig{
		URL: "mysql://root:123456@127.0.0.1:3306/demo?charset=utf8mb4&parseTime=True&loc=Local",
	}
	db, err := New(cfg)
	assert.Nil(t, err)
	assert.NotNil(t, db)

	var result int
	err = db.Raw("SELECT 1").Scan(&result).Error
	assert.Nil(t, err)
	assert.Equal(t, 1, result)

	sqlDB, err := db.DB()
	assert.Nil(t, err)
	err = sqlDB.Close()
	assert.Nil(t, err)
}

func TestNewWithLogConfig(t *testing.T) {
	defer glog.Close()
	customLogCfg := &glog.LogConfig{
		Service:   "custom-service",
		Level:     glog.DebugLevel,
		Writer:    glog.WriterConsole,
		ExtraKeys: []string{glog.KeyRequestId},
	}
	initLogErr := glog.InitLogger(customLogCfg, glog.WithCallerSkip(1))
	assert.Nil(t, initLogErr)

	cfg := &GormConfig{
		URL:     "mysql://root:123456@127.0.0.1:3306/demo?charset=utf8mb4&parseTime=True&loc=Local",
		Service: "test-service",
	}
	db, err := New(cfg, WithLogConfig(customLogCfg))
	assert.Nil(t, err)
	assert.NotNil(t, db)

	var result int
	err = db.Raw("SELECT 1").Scan(&result).Error
	assert.Nil(t, err)
	assert.Equal(t, 1, result)

	sqlDB, err := db.DB()
	assert.Nil(t, err)
	err = sqlDB.Close()
	assert.Nil(t, err)
}

func TestNewPostgres(t *testing.T) {
	defer glog.Close()
	logCfg := &glog.LogConfig{
		Service:   "app",
		Level:     glog.DebugLevel,
		Writer:    glog.WriterConsole,
		ExtraKeys: []string{glog.KeyRequestId},
	}
	initLogErr := glog.InitLogger(logCfg, glog.WithCallerSkip(1))
	assert.Nil(t, initLogErr)

	cfg := &GormConfig{
		URL:             "postgres://postgres:123456@127.0.0.1:5432/demo?sslmode=disable",
		Service:         "test-service",
		MaxIdleConns:    5,
		MaxOpenConns:    50,
		ConnMaxLifetime: time.Hour,
	}
	db, err := New(cfg)
	assert.Nil(t, err)
	assert.NotNil(t, db)

	var result int
	err = db.Raw("SELECT 1").Scan(&result).Error
	assert.Nil(t, err)
	assert.Equal(t, 1, result)

	sqlDB, err := db.DB()
	assert.Nil(t, err)
	err = sqlDB.Close()
	assert.Nil(t, err)
}

func TestNormalizeMySQLURI(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
		hasError bool
	}{
		{
			name:     "standard uri with port and params",
			input:    "mysql://root:123456@127.0.0.1:3306/demo?charset=utf8mb4&parseTime=true",
			expected: "root:123456@tcp(127.0.0.1:3306)/demo?charset=utf8mb4&parseTime=true",
		},
		{
			name:     "uri with default port",
			input:    "mysql://root:123456@127.0.0.1/demo",
			expected: "root:123456@tcp(127.0.0.1:3306)/demo",
		},
		{
			name:     "uri without password",
			input:    "mysql://root@127.0.0.1/demo",
			expected: "root:@tcp(127.0.0.1:3306)/demo",
		},
		{
			name:     "uri with special chars password",
			input:    "mysql://root:p@ssw0rd@127.0.0.1:3307/demo",
			expected: "root:p@ssw0rd@tcp(127.0.0.1:3307)/demo",
		},
		{
			name:     "uri without database",
			input:    "mysql://root:123456@127.0.0.1:3306/",
			expected: "root:123456@tcp(127.0.0.1:3306)/",
		},
		{
			name:     "uri with only query params",
			input:    "mysql://root:123456@127.0.0.1:3307/demo?timeout=5s&readTimeout=3s",
			expected: "root:123456@tcp(127.0.0.1:3307)/demo?timeout=5s&readTimeout=3s",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := normalizeMySQLURI(tt.input)
			if tt.hasError {
				assert.NotNil(t, err)
			} else {
				assert.Nil(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestDetectFromURL(t *testing.T) {
	tests := []struct {
		name         string
		url          string
		expectedType DatabaseType
		hasError     bool
	}{
		{
			name:         "mysql uri format",
			url:          "mysql://root:123456@127.0.0.1:3306/demo",
			expectedType: MySQL,
		},
		{
			name:         "postgres uri format",
			url:          "postgres://postgres:123456@127.0.0.1:5432/demo",
			expectedType: PostgreSQL,
		},
		{
			name:         "postgresql uri format",
			url:          "postgresql://postgres:123456@127.0.0.1:5432/demo",
			expectedType: PostgreSQL,
		},
		{
			name:     "mysql traditional dsn (not supported)",
			url:      "root:123456@tcp(127.0.0.1:3306)/demo",
			hasError: true,
		},
		{
			name:     "postgres kv format (not supported)",
			url:      "host=127.0.0.1 port=5432 user=postgres password=123456 dbname=demo",
			hasError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dialect, err := detectFromURL(tt.url)
			if tt.hasError {
				assert.NotNil(t, err)
				return
			}
			assert.Nil(t, err)
			assert.Equal(t, tt.expectedType, dialect.Name())
		})
	}
}

func TestParseURL(t *testing.T) {
	tests := []struct {
		name       string
		dialect    Dialect
		url        string
		expectedDB string
		hasError   bool
	}{
		{
			name:       "mysql uri format",
			dialect:    &mysqlDialect{},
			url:        "mysql://root:123456@127.0.0.1:3306/demo?charset=utf8mb4",
			expectedDB: "demo",
		},
		{
			name:       "postgres uri format",
			dialect:    &postgresDialect{},
			url:        "postgres://postgres:123456@127.0.0.1:5432/demo?sslmode=disable",
			expectedDB: "demo",
		},
		{
			name:       "postgresql uri format",
			dialect:    &postgresDialect{},
			url:        "postgresql://postgres:123456@127.0.0.1:5432/demo?sslmode=disable",
			expectedDB: "demo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dbName, err := tt.dialect.ParseURL(tt.url)
			if tt.hasError {
				assert.NotNil(t, err)
			} else {
				assert.Nil(t, err)
				assert.Equal(t, tt.expectedDB, dbName)
			}
		})
	}
}

func TestNew_MySQL_URI(t *testing.T) {
	defer glog.Close()
	logCfg := &glog.LogConfig{
		Service:   "app",
		Level:     glog.DebugLevel,
		Writer:    glog.WriterConsole,
		ExtraKeys: []string{glog.KeyRequestId},
	}
	initLogErr := glog.InitLogger(logCfg, glog.WithCallerSkip(1))
	assert.Nil(t, initLogErr)

	cfg := &GormConfig{
		URL: "mysql://root:123456@127.0.0.1:3306/demo?charset=utf8mb4&parseTime=True&loc=Local",
	}
	db, err := New(cfg)
	assert.Nil(t, err)
	assert.NotNil(t, db)

	var result int
	err = db.Raw("SELECT 1").Scan(&result).Error
	assert.Nil(t, err)
	assert.Equal(t, 1, result)

	sqlDB, err := db.DB()
	assert.Nil(t, err)
	assert.NotNil(t, sqlDB)
	err = sqlDB.Close()
	assert.Nil(t, err)
}

func TestNew_PostgreSQL_URI(t *testing.T) {
	defer glog.Close()
	logCfg := &glog.LogConfig{
		Service:   "app",
		Level:     glog.DebugLevel,
		Writer:    glog.WriterConsole,
		ExtraKeys: []string{glog.KeyRequestId},
	}
	initLogErr := glog.InitLogger(logCfg, glog.WithCallerSkip(1))
	assert.Nil(t, initLogErr)

	cfg := &GormConfig{
		URL: "postgres://postgres:123456@127.0.0.1:5432/demo?sslmode=disable",
	}
	db, err := New(cfg)
	assert.Nil(t, err)
	assert.NotNil(t, db)

	var result int
	err = db.Raw("SELECT 1").Scan(&result).Error
	assert.Nil(t, err)
	assert.Equal(t, 1, result)

	sqlDB, err := db.DB()
	assert.Nil(t, err)
	err = sqlDB.Close()
	assert.Nil(t, err)
}

func TestDetectFromURL_InvalidFormat(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		hasError bool
	}{
		{
			name:     "empty url",
			url:      "",
			hasError: true,
		},
		{
			name:     "invalid format without scheme",
			url:      "root:123456@127.0.0.1:3306/demo",
			hasError: true,
		},
		{
			name:     "invalid format with unknown scheme",
			url:      "mongodb://user:pass@host:port/db",
			hasError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := detectFromURL(tt.url)
			if tt.hasError {
				assert.NotNil(t, err)
				t.Logf("error message: %v", err)
			} else {
				assert.Nil(t, err)
			}
		})
	}
}

func TestNewWithInvalidURL(t *testing.T) {
	defer glog.Close()
	logCfg := &glog.LogConfig{
		Service:   "app",
		Level:     glog.DebugLevel,
		Writer:    glog.WriterConsole,
		ExtraKeys: []string{glog.KeyRequestId},
	}
	initLogErr := glog.InitLogger(logCfg, glog.WithCallerSkip(1))
	assert.Nil(t, initLogErr)

	cfg := &GormConfig{
		URL: "invalid-format",
	}
	db, err := New(cfg)
	assert.NotNil(t, err)
	assert.Nil(t, db)
	t.Logf("error message: %v", err)
}
