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

func TestInit(t *testing.T) {
	tempDir := "log/glog-test"
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	t.Run("TestBasicInit", func(t *testing.T) {
		config := &LogConfig{
			Service: "test-service",
			Module:  "test-module",
			Level:   InfoLevel,
			Writer:  WriterFile,
			Dir:     tempDir,
		}

		err := InitLogger(config)
		assert.Nil(t, err)

		Info(context.Background(), "test message")
		Close()

		expectedDir := filepath.Join(tempDir, time.Now().Format("20060102"))
		expectedFile := filepath.Join(expectedDir, "test-service_full.log")
		if !fileExists(expectedFile) {
			t.Errorf("Log file not created: %s", expectedFile)
		}
	})

	t.Run("TestConsoleLogger", func(t *testing.T) {
		config := &LogConfig{
			Service: "test-service",
			Module:  "test-module",
			Level:   InfoLevel,
			Writer:  WriterConsole,
			Dir:     tempDir,
		}

		logger, getLoggerErr := NewLogger(config)
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
	tempDir := "log/glog-test"
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	config := &LogConfig{
		Service: "test",
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

	t.Log("Initializing logger with field hook")
	InitLogger(config, WithFieldHookFunc(phoneDesensitizationHook), WithMessageHookFunc(pwdDesensitizationHook))

	ctx := context.Background()
	t.Log("Logging message with phone number")
	Infow(ctx, "test message", "phone", "13812345678")

	t.Log("Logging message with password")
	Info(ctx, "test message with password=123456")
}

func TestExtraKeys(t *testing.T) {
	tempDir := "log/glog-test"
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	config := &LogConfig{
		Service:   "test",
		Module:    "test",
		Level:     DebugLevel,
		Writer:    WriterConsole,
		Dir:       tempDir,
		ExtraKeys: []string{KeyTraceID, "user_id", KeyAppRequestID},
	}

	t.Log("Initializing logger with extra keys")

	logger, getLoggerErr := NewLogger(config)
	if getLoggerErr != nil {
		t.Fatalf("failed to get logger: %v", getLoggerErr)
	}

	ctx := context.Background()
	ctx = context.WithValue(ctx, KeyTraceID, "123456")
	ctx = context.WithValue(ctx, "user_id", "user123")
	ctx = context.WithValue(ctx, KeyAppRequestID, "req789")
	ctx = context.WithValue(ctx, "other_field", "should_not_appear")

	t.Log("Logging message with extra fields")
	logger.Infow(ctx, "test message with extra fields", "key", "value")

	Close()
}

func TestLogRotation(t *testing.T) {
	tempDir := "log/glog-rotation-test"
	defer os.RemoveAll(tempDir)

	config := &LogConfig{
		Service:    "rotation-test",
		Level:      InfoLevel,
		Writer:     WriterFile,
		Dir:        tempDir,
		MaxSize:    1,
		MaxBackups: 5,
		MaxAge:     7,
		Compress:   false,
	}

	err := InitLogger(config)
	assert.Nil(t, err)

	ctx := context.Background()

	largeMessage := strings.Repeat("x", 200*1024)
	for i := 0; i < 10; i++ {
		Info(ctx, fmt.Sprintf("large message %d: %s", i, largeMessage))
	}

	time.Sleep(2 * time.Second)

	expectedDir := filepath.Join(tempDir, time.Now().Format("20060102"))
	baseFile := filepath.Join(expectedDir, "rotation-test_full.log")

	assert.True(t, fileExists(baseFile), "Current log file should exist")

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

	Close()
}

func TestOTELTraceFieldsInjected(t *testing.T) {
	tempDir := "log/glog-otel-test"
	defer os.RemoveAll(tempDir)

	config := &LogConfig{
		Service:         "otel-test",
		Module:          "test",
		Level:           InfoLevel,
		Writer:          WriterFile,
		Dir:             tempDir,
		EnableOTELTrace: true,
	}

	logger, err := NewLogger(config)
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

	assert.Contains(t, content, KeyTraceID)
	assert.Contains(t, content, KeySpanID)
	assert.Contains(t, content, KeyTraceFlags)
}

func TestOTELTraceFieldsDisabled(t *testing.T) {
	tempDir := "log/glog-otel-disabled-test"
	defer os.RemoveAll(tempDir)

	config := &LogConfig{
		Service:         "otel-disabled",
		Module:          "test",
		Level:           InfoLevel,
		Writer:          WriterFile,
		Dir:             tempDir,
		EnableOTELTrace: false,
	}

	logger, err := NewLogger(config)
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

	assert.NotContains(t, content, `"`+KeyTraceID+`"`)
	assert.NotContains(t, content, `"`+KeySpanID+`"`)
	assert.NotContains(t, content, `"`+KeyTraceFlags+`"`)
}

func TestOTELTraceOptionOverridesConfig(t *testing.T) {
	tempDir := "log/glog-otel-option-test"
	defer os.RemoveAll(tempDir)

	config := &LogConfig{
		Service:         "otel-option",
		Module:          "test",
		Level:           InfoLevel,
		Writer:          WriterFile,
		Dir:             tempDir,
		EnableOTELTrace: true,
	}

	logger, err := NewLogger(config, WithOTELTrace(false))
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

	assert.NotContains(t, content, `"`+KeyTraceID+`"`)
	assert.NotContains(t, content, `"`+KeySpanID+`"`)
	assert.NotContains(t, content, `"`+KeyTraceFlags+`"`)
}

func TestOTELTraceWithoutSpanContext(t *testing.T) {
	tempDir := "log/glog-otel-nospan-test"
	defer os.RemoveAll(tempDir)

	config := &LogConfig{
		Service:         "otel-nospan",
		Module:          "test",
		Level:           InfoLevel,
		Writer:          WriterFile,
		Dir:             tempDir,
		EnableOTELTrace: true,
	}

	logger, err := NewLogger(config)
	assert.Nil(t, err)

	logger.Infow(context.Background(), "without span context", "key", "value")
	logger.Close()

	logFile := filepath.Join(tempDir, time.Now().Format("20060102"), "otel-nospan_full.log")
	b, readErr := os.ReadFile(logFile)
	assert.Nil(t, readErr)
	content := string(b)

	assert.NotContains(t, content, `"`+KeyTraceID+`"`)
	assert.NotContains(t, content, `"`+KeySpanID+`"`)
	assert.NotContains(t, content, `"`+KeyTraceFlags+`"`)
}