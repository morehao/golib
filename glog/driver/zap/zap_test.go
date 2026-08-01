package zap_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/morehao/golib/glog"
	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel/sdk/trace"
)

func TestInit(t *testing.T) {
	tempDir := "log/glog-test"
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	t.Run("TestBasicInit", func(t *testing.T) {
		config := &glog.LogConfig{
			Service:    "test-service",
			Module:     "test-module",
			Level:      glog.InfoLevel,
			Writers:    []glog.WriterConfig{{Type: glog.WriterFile, Dir: tempDir}},
			LoggerType: glog.LoggerTypeZap,
		}

		err := glog.InitLogger(config)
		assert.Nil(t, err)

		glog.Info(context.Background(), "test message")
		glog.Close()

		expectedDir := filepath.Join(tempDir, time.Now().Format("20060102"))
		expectedFile := filepath.Join(expectedDir, "test-service_full.log")
		if !glog.FileExists(expectedFile) {
			t.Errorf("Log file not created: %s", expectedFile)
		}
	})

	t.Run("TestConsoleLogger", func(t *testing.T) {
		config := &glog.LogConfig{
			Service:    "test-service",
			Module:     "test-module",
			Level:      glog.InfoLevel,
			Writers:    []glog.WriterConfig{{Type: glog.WriterConsole}},
			LoggerType: glog.LoggerTypeZap,
		}

		logger, getLoggerErr := glog.NewLogger(config)
		assert.Nil(t, getLoggerErr)
		if logger == nil {
			t.Error("Console logger not initialized")
		}

		ctx := context.Background()
		logger.Debug(ctx, "debug to console")
		logger.Info(ctx, "info to console")
	})
}

func TestHook(t *testing.T) {
	tempDir := t.TempDir()
	config := &glog.LogConfig{
		Service:    "test",
		Level:      glog.DebugLevel,
		ExtraKeys:  []string{"phone"},
		LoggerType: glog.LoggerTypeZap,
		Writers:    []glog.WriterConfig{{Type: glog.WriterFile, Dir: tempDir}},
	}

	var phoneDesensitizationHook = func(fields []glog.Field) {
		phoneRegex := regexp.MustCompile(`(\d{3})\d{4}(\d{4})`)
		for i := range fields {
			if fields[i].Key == "phone" {
				if strValue, ok := fields[i].Value.(string); ok {
					fields[i].Value = phoneRegex.ReplaceAllString(strValue, `$1****$2`)
				}
			}
		}
	}

	var pwdDesensitizationHook = func(message string) string {
		if strings.Contains(message, "password") {
			re := regexp.MustCompile(`password=[^&\s]+`)
			return re.ReplaceAllString(message, "password=***")
		}
		return message
	}

	logger, err := glog.NewLogger(config, glog.WithFieldHookFunc(phoneDesensitizationHook), glog.WithMessageHookFunc(pwdDesensitizationHook))
	assert.Nil(t, err)

	ctx := context.Background()
	logger.Infow(ctx, "test message", "phone", "13812345678")
	ctx = context.WithValue(ctx, "phone", "13812345678")
	logger.Info(ctx, "ctx phone message")
	logger.Info(ctx, "test message with password=123456")
	logger.Close()

	dateStr := time.Now().Format("20060102")
	b, readErr := os.ReadFile(filepath.Join(tempDir, dateStr, "test_full.log"))
	assert.Nil(t, readErr)
	content := string(b)
	assert.Contains(t, content, "138****5678")
	assert.NotContains(t, content, "13812345678")
	assert.Contains(t, content, "password=***")
	assert.NotContains(t, content, "password=123456")
}

func TestExtraKeys(t *testing.T) {
	tempDir := "log/glog-test"
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	config := &glog.LogConfig{
		Service:    "test",
		Module:     "test",
		Level:      glog.DebugLevel,
		Writers:    []glog.WriterConfig{{Type: glog.WriterConsole}},
		ExtraKeys:  []string{glog.KeyTraceID, "user_id", glog.KeyAppRequestID},
		LoggerType: glog.LoggerTypeZap,
	}

	t.Log("Initializing logger with extra keys")

	logger, getLoggerErr := glog.NewLogger(config)
	if getLoggerErr != nil {
		t.Fatalf("failed to get logger: %v", getLoggerErr)
	}

	ctx := context.Background()
	ctx = context.WithValue(ctx, glog.KeyTraceID, "123456")
	ctx = context.WithValue(ctx, "user_id", "user123")
	ctx = context.WithValue(ctx, glog.KeyAppRequestID, "req789")
	ctx = context.WithValue(ctx, "other_field", "should_not_appear")

	t.Log("Logging message with extra fields")
	logger.Infow(ctx, "test message with extra fields", "key", "value")

	glog.Close()
}

func TestLogRotation(t *testing.T) {
	tempDir := "log/glog-rotation-test"
	defer os.RemoveAll(tempDir)

	config := &glog.LogConfig{
		Service:    "rotation-test",
		Level:      glog.InfoLevel,
		Writers:    []glog.WriterConfig{{Type: glog.WriterFile, Dir: tempDir, MaxSize: 1, MaxBackups: 5, MaxAge: 7, Compress: false}},
		LoggerType: glog.LoggerTypeZap,
	}

	err := glog.InitLogger(config)
	assert.Nil(t, err)

	ctx := context.Background()

	largeMessage := strings.Repeat("x", 200*1024)
	for i := 0; i < 10; i++ {
		glog.Info(ctx, fmt.Sprintf("large message %d: %s", i, largeMessage))
	}

	time.Sleep(2 * time.Second)

	expectedDir := filepath.Join(tempDir, time.Now().Format("20060102"))
	baseFile := filepath.Join(expectedDir, "rotation-test_full.log")

	assert.True(t, glog.FileExists(baseFile), "Current log file should exist")

	files, err := os.ReadDir(expectedDir)
	assert.Nil(t, err)

	rotated := false
	for _, file := range files {
		if strings.Contains(file.Name(), "rotation-test_full-") && strings.HasSuffix(file.Name(), ".log") {
			rotated = true
			break
		}
	}

	assert.True(t, rotated, "Log rotation should occur when file size exceeds MaxSize")

	glog.Close()
}

func TestOTELTraceFieldsInjected(t *testing.T) {
	tempDir := "log/glog-otel-test"
	defer os.RemoveAll(tempDir)

	config := &glog.LogConfig{
		Service:         "otel-test",
		Module:          "test",
		Level:           glog.InfoLevel,
		Writers:         []glog.WriterConfig{{Type: glog.WriterFile, Dir: tempDir}},
		EnableOTELTrace: true,
		LoggerType:      glog.LoggerTypeZap,
	}

	logger, err := glog.NewLogger(config)
	assert.Nil(t, err)

	tp := trace.NewTracerProvider()
	defer func() {
		_ = tp.Shutdown(context.Background())
	}()

	ctx, span := tp.Tracer("glog-test").Start(context.Background(), "test-span")
	logger.Infow(ctx, "otel trace fields", "key", "value")
	span.End()
	logger.Close()

	logFile := filepath.Join(tempDir, time.Now().Format("20060102"), "otel-test_full.log")
	b, readErr := os.ReadFile(logFile)
	assert.Nil(t, readErr)
	content := string(b)

	assert.Contains(t, content, glog.KeyTraceID)
	assert.Contains(t, content, glog.KeySpanID)
	assert.Contains(t, content, glog.KeyTraceFlags)
}

func TestOTELTraceFieldsDisabled(t *testing.T) {
	tempDir := "log/glog-otel-disabled-test"
	defer os.RemoveAll(tempDir)

	config := &glog.LogConfig{
		Service:         "otel-disabled",
		Module:          "test",
		Level:           glog.InfoLevel,
		Writers:         []glog.WriterConfig{{Type: glog.WriterFile, Dir: tempDir}},
		EnableOTELTrace: false,
		LoggerType:      glog.LoggerTypeZap,
	}

	logger, err := glog.NewLogger(config)
	assert.Nil(t, err)

	tp := trace.NewTracerProvider()
	defer func() {
		_ = tp.Shutdown(context.Background())
	}()

	ctx, span := tp.Tracer("glog-test").Start(context.Background(), "test-span")
	logger.Infow(ctx, "otel trace fields disabled", "key", "value")
	span.End()
	logger.Close()

	logFile := filepath.Join(tempDir, time.Now().Format("20060102"), "otel-disabled_full.log")
	b, readErr := os.ReadFile(logFile)
	assert.Nil(t, readErr)
	content := string(b)

	assert.NotContains(t, content, `"`+glog.KeyTraceID+`"`)
	assert.NotContains(t, content, `"`+glog.KeySpanID+`"`)
	assert.NotContains(t, content, `"`+glog.KeyTraceFlags+`"`)
}

func TestOTELTraceOptionOverridesConfig(t *testing.T) {
	tempDir := "log/glog-otel-option-test"
	defer os.RemoveAll(tempDir)

	config := &glog.LogConfig{
		Service:         "otel-option",
		Module:          "test",
		Level:           glog.InfoLevel,
		Writers:         []glog.WriterConfig{{Type: glog.WriterFile, Dir: tempDir}},
		EnableOTELTrace: true,
		LoggerType:      glog.LoggerTypeZap,
	}

	logger, err := glog.NewLogger(config, glog.WithOTELTrace(false))
	assert.Nil(t, err)

	tp := trace.NewTracerProvider()
	defer func() {
		_ = tp.Shutdown(context.Background())
	}()

	ctx, span := tp.Tracer("glog-test").Start(context.Background(), "test-span")
	logger.Infow(ctx, "otel trace option override", "key", "value")
	span.End()
	logger.Close()

	logFile := filepath.Join(tempDir, time.Now().Format("20060102"), "otel-option_full.log")
	b, readErr := os.ReadFile(logFile)
	assert.Nil(t, readErr)
	content := string(b)

	assert.NotContains(t, content, `"`+glog.KeyTraceID+`"`)
	assert.NotContains(t, content, `"`+glog.KeySpanID+`"`)
	assert.NotContains(t, content, `"`+glog.KeyTraceFlags+`"`)
}

func TestOTELTraceWithoutSpanContext(t *testing.T) {
	tempDir := "log/glog-otel-nospan-test"
	defer os.RemoveAll(tempDir)

	config := &glog.LogConfig{
		Service:         "otel-nospan",
		Module:          "test",
		Level:           glog.InfoLevel,
		Writers:         []glog.WriterConfig{{Type: glog.WriterFile, Dir: tempDir}},
		EnableOTELTrace: true,
		LoggerType:      glog.LoggerTypeZap,
	}

	logger, err := glog.NewLogger(config)
	assert.Nil(t, err)

	logger.Infow(context.Background(), "without span context", "key", "value")
	logger.Close()

	logFile := filepath.Join(tempDir, time.Now().Format("20060102"), "otel-nospan_full.log")
	b, readErr := os.ReadFile(logFile)
	assert.Nil(t, readErr)
	content := string(b)

	assert.NotContains(t, content, `"`+glog.KeyTraceID+`"`)
	assert.NotContains(t, content, `"`+glog.KeySpanID+`"`)
	assert.NotContains(t, content, `"`+glog.KeyTraceFlags+`"`)
}

func TestZapMultiWritersFileAndConsole(t *testing.T) {
	tempDir := t.TempDir()
	config := &glog.LogConfig{
		Service:    "zap-multi",
		Module:     "test",
		Level:      glog.InfoLevel,
		LoggerType: glog.LoggerTypeZap,
		Writers: []glog.WriterConfig{
			{Type: glog.WriterConsole, Level: glog.DebugLevel},
			{Type: glog.WriterFile, Dir: tempDir, MaxSize: 100, MaxBackups: 3, MaxAge: 7},
		},
	}
	logger, err := glog.NewLogger(config)
	assert.Nil(t, err)
	ctx := context.Background()
	logger.Info(ctx, "multi writer test")
	logger.Close()
	dateStr := time.Now().Format("20060102")
	expectedFile := filepath.Join(tempDir, dateStr, "zap-multi_full.log")
	assert.True(t, glog.FileExists(expectedFile), expectedFile)
}

func TestZapMultiWritersLevelSplit(t *testing.T) {
	tempDir := t.TempDir()
	config := &glog.LogConfig{
		Service:    "zap-split",
		Module:     "test",
		Level:      glog.InfoLevel,
		LoggerType: glog.LoggerTypeZap,
		Writers: []glog.WriterConfig{
			{Type: glog.WriterConsole, Level: glog.DebugLevel},
			{Type: glog.WriterFile, Dir: tempDir, MaxSize: 100, MaxBackups: 3, MaxAge: 7},
			{Type: glog.WriterFile, Dir: tempDir, FileName: "error.log", WfOnly: true},
		},
	}
	logger, err := glog.NewLogger(config)
	assert.Nil(t, err)
	ctx := context.Background()
	logger.Info(ctx, "info message")
	logger.Error(ctx, "error message")
	logger.Close()
	dateStr := time.Now().Format("20060102")
	fullFile := filepath.Join(tempDir, dateStr, "zap-split_full.log")
	b, _ := os.ReadFile(fullFile)
	content := string(b)
	assert.Contains(t, content, "info message")
	assert.Contains(t, content, "error message")
	errorFile := filepath.Join(tempDir, dateStr, "error_wf.log")
	b2, _ := os.ReadFile(errorFile)
	content2 := string(b2)
	assert.NotContains(t, content2, "info message")
	assert.Contains(t, content2, "error message")
}

func TestZapCloseFlushesBuffer(t *testing.T) {
	tempDir := t.TempDir()
	config := &glog.LogConfig{
		Service:    "zap-flush",
		Module:     "test",
		Level:      glog.InfoLevel,
		LoggerType: glog.LoggerTypeZap,
		Writers:    []glog.WriterConfig{{Type: glog.WriterFile, Dir: tempDir}},
	}
	logger, err := glog.NewLogger(config)
	assert.Nil(t, err)
	logger.Info(context.Background(), "buffered message")
	logger.Close()
	dateStr := time.Now().Format("20060102")
	b, readErr := os.ReadFile(filepath.Join(tempDir, dateStr, "zap-flush_full.log"))
	assert.Nil(t, readErr)
	assert.Contains(t, string(b), "buffered message")
}
