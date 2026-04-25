package glog

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel/sdk/trace"
)

func testSlogLoggerConfig() *LogConfig {
	return &LogConfig{
		Service: "slog-test",
		Module:  "test-module",
		Level:   InfoLevel,
		Writer:  WriterConsole,
		Dir:     "log/slog-test",
	}
}

func TestSlogLoggerInit(t *testing.T) {
	tempDir := "log/slog-test-init"
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	t.Run("TestSlogBasicInit", func(t *testing.T) {
		config := &LogConfig{
			Service: "slog-service",
			Module:  "slog-module",
			Level:   InfoLevel,
			Writer:  WriterFile,
			Dir:     tempDir,
		}

		err := InitLogger(config, WithLoggerType(LoggerTypeSlog))
		assert.Nil(t, err)

		Info(context.Background(), "slog test message")

		expectedDir := filepath.Join(tempDir, time.Now().Format("20060102"))
		expectedFile := filepath.Join(expectedDir, "slog-service_full.log")
		if !fileExists(expectedFile) {
			t.Errorf("Log file not created: %s", expectedFile)
		}
	})

	t.Run("TestSlogConsoleLogger", func(t *testing.T) {
		config := &LogConfig{
			Service: "slog-service",
			Module:  "slog-module",
			Level:   InfoLevel,
			Writer:  WriterConsole,
			Dir:     tempDir,
		}

		logger, getLoggerErr := newSlogLogger(config)
		assert.Nil(t, getLoggerErr)
		if logger == nil {
			t.Error("Slog console logger not initialized")
		}

		ctx := context.Background()
		logger.Debug(ctx, "debug to console")
		logger.Info(ctx, "info to console")
	})
}

func TestSlogLoggerLevels(t *testing.T) {
	config := testSlogLoggerConfig()
	logger, err := newSlogLogger(config)
	assert.Nil(t, err)

	ctx := context.Background()
	logger.Debug(ctx, "debug message")
	logger.Info(ctx, "info message")
	logger.Warn(ctx, "warn message")
	logger.Error(ctx, "error message")

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic after Panic")
		}
	}()
	logger.Panic(ctx, "fatal message")
}

func TestSlogLoggerWithFields(t *testing.T) {
	config := testSlogLoggerConfig()
	logger, err := newSlogLogger(config)
	assert.Nil(t, err)

	ctx := context.Background()
	logger.Infow(ctx, "info with fields", "key1", "value1", "key2", "value2")
	logger.Errorw(ctx, "error with fields", "error", "something went wrong", "code", 500)
}

func TestSlogLoggerFormat(t *testing.T) {
	config := testSlogLoggerConfig()
	logger, err := newSlogLogger(config)
	assert.Nil(t, err)

	ctx := context.Background()
	logger.Debugf(ctx, "debug format: %s", "value")
	logger.Infof(ctx, "info format: %s", "value")
	logger.Warnf(ctx, "warn format: %s", "value")
	logger.Errorf(ctx, "error format: %s", "value")
}

func TestSlogLoggerHook(t *testing.T) {
	tempDir := "log/slog-hook-test"
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	config := &LogConfig{
		Service: "slog-hook",
		Level:   DebugLevel,
		Writer:  WriterConsole,
		Dir:     tempDir,
	}

	var phoneDesensitizationHook = func(fields []Field) {
		phoneRegex := regexp.MustCompile(`(\d{3})\d{4}(\d{4})`)
		for i := range fields {
			if fields[i].Key == "phone" {
				strValue, ok := fields[i].Value.(string)
				if ok {
					if phoneRegex.MatchString(strValue) {
						fields[i].Value = phoneRegex.ReplaceAllString(strValue, `$1****$2`)
						t.Log("Phone number desensitized:", fields[i].Value)
					}
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

	logger, err := newSlogLogger(config, WithLoggerType(LoggerTypeSlog), WithFieldHookFunc(phoneDesensitizationHook), WithMessageHookFunc(pwdDesensitizationHook))
	assert.Nil(t, err)

	ctx := context.Background()
	logger.Infow(ctx, "test message", "phone", "13812345678")
	logger.Info(ctx, "test message with password=123456")
}

func TestSlogLoggerExtraKeys(t *testing.T) {
	tempDir := "log/slog-extrakeys-test"
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	config := &LogConfig{
		Service:   "slog-extrakeys",
		Module:    "test",
		Level:     DebugLevel,
		Writer:    WriterConsole,
		Dir:       tempDir,
		ExtraKeys: []string{KeyTraceID, "user_id", KeyAppRequestID},
	}

	logger, err := newSlogLogger(config)
	assert.Nil(t, err)

	ctx := context.Background()
	ctx = context.WithValue(ctx, KeyTraceID, "123456")
	ctx = context.WithValue(ctx, "user_id", "user123")
	ctx = context.WithValue(ctx, KeyAppRequestID, "req789")
	ctx = context.WithValue(ctx, "other_field", "should_not_appear")

	logger.Infow(ctx, "test message with extra fields", "key", "value")

	logger.Close()
}

func TestSlogLoggerOTELTrace(t *testing.T) {
	tempDir := "log/slog-otel-test"
	defer os.RemoveAll(tempDir)

	config := &LogConfig{
		Service:         "slog-otel",
		Module:          "test",
		Level:           InfoLevel,
		Writer:          WriterFile,
		Dir:             tempDir,
		EnableOTELTrace: true,
	}

	logger, err := newSlogLogger(config)
	assert.Nil(t, err)

	tp := trace.NewTracerProvider()
	defer func() {
		_ = tp.Shutdown(context.Background())
	}()

	ctx, span := tp.Tracer("glog-test").Start(context.Background(), "test-span")
	logger.Infow(ctx, "otel trace fields", "key", "value")
	span.End()
	logger.Close()

	logFile := filepath.Join(tempDir, time.Now().Format("20060102"), "slog-otel_full.log")
	b, readErr := os.ReadFile(logFile)
	assert.Nil(t, readErr)
	content := string(b)

	assert.Contains(t, content, KeyTraceID)
	assert.Contains(t, content, KeySpanID)
	assert.Contains(t, content, KeyTraceFlags)
}

func TestSlogLoggerOTELTraceDisabled(t *testing.T) {
	tempDir := "log/slog-otel-disabled-test"
	defer os.RemoveAll(tempDir)

	config := &LogConfig{
		Service:         "slog-otel-disabled",
		Module:          "test",
		Level:           InfoLevel,
		Writer:          WriterFile,
		Dir:             tempDir,
		EnableOTELTrace: false,
	}

	logger, err := newSlogLogger(config)
	assert.Nil(t, err)

	tp := trace.NewTracerProvider()
	defer func() {
		_ = tp.Shutdown(context.Background())
	}()

	ctx, span := tp.Tracer("glog-test").Start(context.Background(), "test-span")
	logger.Infow(ctx, "otel trace fields disabled", "key", "value")
	span.End()
	logger.Close()

	logFile := filepath.Join(tempDir, time.Now().Format("20060102"), "slog-otel-disabled_full.log")
	b, readErr := os.ReadFile(logFile)
	assert.Nil(t, readErr)
	content := string(b)

	assert.NotContains(t, content, `"`+KeyTraceID+`"`)
	assert.NotContains(t, content, `"`+KeySpanID+`"`)
	assert.NotContains(t, content, `"`+KeyTraceFlags+`"`)
}

func TestSlogLoggerOTELTraceOptionOverridesConfig(t *testing.T) {
	tempDir := "log/slog-otel-option-test"
	defer os.RemoveAll(tempDir)

	config := &LogConfig{
		Service:         "slog-otel-option",
		Module:          "test",
		Level:           InfoLevel,
		Writer:          WriterFile,
		Dir:             tempDir,
		EnableOTELTrace: true,
	}

	logger, err := newSlogLogger(config, WithLoggerType(LoggerTypeSlog), WithOTELTrace(false))
	assert.Nil(t, err)

	tp := trace.NewTracerProvider()
	defer func() {
		_ = tp.Shutdown(context.Background())
	}()

	ctx, span := tp.Tracer("glog-test").Start(context.Background(), "test-span")
	logger.Infow(ctx, "otel trace option override", "key", "value")
	span.End()
	logger.Close()

	logFile := filepath.Join(tempDir, time.Now().Format("20060102"), "slog-otel-option_full.log")
	b, readErr := os.ReadFile(logFile)
	assert.Nil(t, readErr)
	content := string(b)

	assert.NotContains(t, content, `"`+KeyTraceID+`"`)
	assert.NotContains(t, content, `"`+KeySpanID+`"`)
	assert.NotContains(t, content, `"`+KeyTraceFlags+`"`)
}

func TestSlogLoggerOTELTraceWithoutSpanContext(t *testing.T) {
	tempDir := "log/slog-otel-nospan-test"
	defer os.RemoveAll(tempDir)

	config := &LogConfig{
		Service:         "slog-otel-nospan",
		Module:          "test",
		Level:           InfoLevel,
		Writer:          WriterFile,
		Dir:             tempDir,
		EnableOTELTrace: true,
	}

	logger, err := newSlogLogger(config)
	assert.Nil(t, err)

	logger.Infow(context.Background(), "without span context", "key", "value")
	logger.Close()

	logFile := filepath.Join(tempDir, time.Now().Format("20060102"), "slog-otel-nospan_full.log")
	b, readErr := os.ReadFile(logFile)
	assert.Nil(t, readErr)
	content := string(b)

	assert.NotContains(t, content, `"`+KeyTraceID+`"`)
	assert.NotContains(t, content, `"`+KeySpanID+`"`)
	assert.NotContains(t, content, `"`+KeyTraceFlags+`"`)
}

func TestSlogLoggerRotation(t *testing.T) {
	tempDir := "log/slog-rotation-test"
	defer os.RemoveAll(tempDir)

	config := &LogConfig{
		Service:    "slog-rotation-test",
		Level:      InfoLevel,
		Writer:     WriterFile,
		Dir:        tempDir,
		MaxSize:    1,
		MaxBackups: 5,
		MaxAge:     7,
		Compress:   false,
	}

	err := InitLogger(config, WithLoggerType(LoggerTypeSlog))
	assert.Nil(t, err)

	ctx := context.Background()

	largeMessage := strings.Repeat("x", 200*1024)
	for i := 0; i < 10; i++ {
		Info(ctx, fmt.Sprintf("large message %d: %s", i, largeMessage))
	}

	time.Sleep(2 * time.Second)

	expectedDir := filepath.Join(tempDir, time.Now().Format("20060102"))
	baseFile := filepath.Join(expectedDir, "slog-rotation-test_full.log")

	assert.True(t, fileExists(baseFile), "Current log file should exist")

	files, err := os.ReadDir(expectedDir)
	assert.Nil(t, err)

	rotated := false
	for _, file := range files {
		if strings.Contains(file.Name(), "slog-rotation-test_full-") && strings.HasSuffix(file.Name(), ".log") {
			rotated = true
			break
		}
	}

	assert.True(t, rotated, "Log rotation should occur when file size exceeds MaxSize")

	Close()
}