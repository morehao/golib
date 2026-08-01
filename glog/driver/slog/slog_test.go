package slog_test

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
	_ "github.com/morehao/golib/glog/driver/slog"
	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel/sdk/trace"
)

func TestSlogLoggerInit(t *testing.T) {
	tempDir := "log/slog-test-init"
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	t.Run("TestSlogBasicInit", func(t *testing.T) {
		config := &glog.LogConfig{
			Service:    "slog-service",
			Module:     "slog-module",
			Level:      glog.InfoLevel,
			Writers:    []glog.WriterConfig{{Type: glog.WriterFile, Dir: tempDir}},
			LoggerType: glog.LoggerTypeSlog,
		}

		err := glog.InitLogger(config)
		assert.Nil(t, err)

		glog.Info(context.Background(), "slog test message")

		expectedDir := filepath.Join(tempDir, time.Now().Format("20060102"))
		expectedFile := filepath.Join(expectedDir, "slog-service_full.log")
		if !glog.FileExists(expectedFile) {
			t.Errorf("Log file not created: %s", expectedFile)
		}
	})

	t.Run("TestSlogConsoleLogger", func(t *testing.T) {
		config := &glog.LogConfig{
			Service:    "slog-service",
			Module:     "slog-module",
			Level:      glog.InfoLevel,
			Writers:    []glog.WriterConfig{{Type: glog.WriterConsole}},
			LoggerType: glog.LoggerTypeSlog,
		}

		logger, getLoggerErr := glog.NewLogger(config)
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
	config := &glog.LogConfig{
		Service:    "slog-test",
		Module:     "test-module",
		Level:      glog.InfoLevel,
		Writers:    []glog.WriterConfig{{Type: glog.WriterConsole}},
		LoggerType: glog.LoggerTypeSlog,
	}
	logger, err := glog.NewLogger(config)
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
	config := &glog.LogConfig{
		Service:    "slog-test",
		Module:     "test-module",
		Level:      glog.InfoLevel,
		Writers:    []glog.WriterConfig{{Type: glog.WriterConsole}},
		LoggerType: glog.LoggerTypeSlog,
	}
	logger, err := glog.NewLogger(config)
	assert.Nil(t, err)

	ctx := context.Background()
	logger.Infow(ctx, "info with fields", "key1", "value1", "key2", "value2")
	logger.Errorw(ctx, "error with fields", "error", "something went wrong", "code", 500)
}

func TestSlogLoggerFormat(t *testing.T) {
	config := &glog.LogConfig{
		Service:    "slog-test",
		Module:     "test-module",
		Level:      glog.InfoLevel,
		Writers:    []glog.WriterConfig{{Type: glog.WriterConsole}},
		LoggerType: glog.LoggerTypeSlog,
	}
	logger, err := glog.NewLogger(config)
	assert.Nil(t, err)

	ctx := context.Background()
	logger.Debugf(ctx, "debug format: %s", "value")
	logger.Infof(ctx, "info format: %s", "value")
	logger.Warnf(ctx, "warn format: %s", "value")
	logger.Errorf(ctx, "error format: %s", "value")
}

func TestSlogLoggerHook(t *testing.T) {
	tempDir := t.TempDir()
	config := &glog.LogConfig{
		Service:    "slog-hook",
		Level:      glog.DebugLevel,
		LoggerType: glog.LoggerTypeSlog,
		ExtraKeys:  []string{"phone"},
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
	b, readErr := os.ReadFile(filepath.Join(tempDir, dateStr, "slog-hook_full.log"))
	assert.Nil(t, readErr)
	content := string(b)
	assert.Contains(t, content, "138****5678")
	assert.NotContains(t, content, "13812345678")
	assert.Contains(t, content, "password=***")
	assert.NotContains(t, content, "password=123456")
}

func TestSlogLoggerExtraKeys(t *testing.T) {
	tempDir := "log/slog-extrakeys-test"
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	config := &glog.LogConfig{
		Service:    "slog-extrakeys",
		Module:     "test",
		Level:      glog.DebugLevel,
		Writers:    []glog.WriterConfig{{Type: glog.WriterConsole}},
		ExtraKeys:  []string{glog.KeyTraceID, "user_id", glog.KeyAppRequestID},
		LoggerType: glog.LoggerTypeSlog,
	}

	logger, err := glog.NewLogger(config)
	assert.Nil(t, err)

	ctx := context.Background()
	ctx = context.WithValue(ctx, glog.KeyTraceID, "123456")
	ctx = context.WithValue(ctx, "user_id", "user123")
	ctx = context.WithValue(ctx, glog.KeyAppRequestID, "req789")
	ctx = context.WithValue(ctx, "other_field", "should_not_appear")

	logger.Infow(ctx, "test message with extra fields", "key", "value")

	logger.Close()
}

func TestSlogLoggerOTELTrace(t *testing.T) {
	tempDir := "log/slog-otel-test"
	defer os.RemoveAll(tempDir)

	config := &glog.LogConfig{
		Service:         "slog-otel",
		Module:          "test",
		Level:           glog.InfoLevel,
		Writers:         []glog.WriterConfig{{Type: glog.WriterFile, Dir: tempDir}},
		EnableOTELTrace: true,
		LoggerType:      glog.LoggerTypeSlog,
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

	logFile := filepath.Join(tempDir, time.Now().Format("20060102"), "slog-otel_full.log")
	b, readErr := os.ReadFile(logFile)
	assert.Nil(t, readErr)
	content := string(b)

	assert.Contains(t, content, glog.KeyTraceID)
	assert.Contains(t, content, glog.KeySpanID)
	assert.Contains(t, content, glog.KeyTraceFlags)
}

func TestSlogLoggerOTELTraceDisabled(t *testing.T) {
	tempDir := "log/slog-otel-disabled-test"
	defer os.RemoveAll(tempDir)

	config := &glog.LogConfig{
		Service:         "slog-otel-disabled",
		Module:          "test",
		Level:           glog.InfoLevel,
		Writers:         []glog.WriterConfig{{Type: glog.WriterFile, Dir: tempDir}},
		EnableOTELTrace: false,
		LoggerType:      glog.LoggerTypeSlog,
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

	logFile := filepath.Join(tempDir, time.Now().Format("20060102"), "slog-otel-disabled_full.log")
	b, readErr := os.ReadFile(logFile)
	assert.Nil(t, readErr)
	content := string(b)

	assert.NotContains(t, content, `"`+glog.KeyTraceID+`"`)
	assert.NotContains(t, content, `"`+glog.KeySpanID+`"`)
	assert.NotContains(t, content, `"`+glog.KeyTraceFlags+`"`)
}

func TestSlogLoggerOTELTraceOptionOverridesConfig(t *testing.T) {
	tempDir := "log/slog-otel-option-test"
	defer os.RemoveAll(tempDir)

	config := &glog.LogConfig{
		Service:         "slog-otel-option",
		Module:          "test",
		Level:           glog.InfoLevel,
		Writers:         []glog.WriterConfig{{Type: glog.WriterFile, Dir: tempDir}},
		EnableOTELTrace: true,
		LoggerType:      glog.LoggerTypeSlog,
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

	logFile := filepath.Join(tempDir, time.Now().Format("20060102"), "slog-otel-option_full.log")
	b, readErr := os.ReadFile(logFile)
	assert.Nil(t, readErr)
	content := string(b)

	assert.NotContains(t, content, `"`+glog.KeyTraceID+`"`)
	assert.NotContains(t, content, `"`+glog.KeySpanID+`"`)
	assert.NotContains(t, content, `"`+glog.KeyTraceFlags+`"`)
}

func TestSlogLoggerOTELTraceWithoutSpanContext(t *testing.T) {
	tempDir := "log/slog-otel-nospan-test"
	defer os.RemoveAll(tempDir)

	config := &glog.LogConfig{
		Service:         "slog-otel-nospan",
		Module:          "test",
		Level:           glog.InfoLevel,
		Writers:         []glog.WriterConfig{{Type: glog.WriterFile, Dir: tempDir}},
		EnableOTELTrace: true,
		LoggerType:      glog.LoggerTypeSlog,
	}

	logger, err := glog.NewLogger(config)
	assert.Nil(t, err)

	logger.Infow(context.Background(), "without span context", "key", "value")
	logger.Close()

	logFile := filepath.Join(tempDir, time.Now().Format("20060102"), "slog-otel-nospan_full.log")
	b, readErr := os.ReadFile(logFile)
	assert.Nil(t, readErr)
	content := string(b)

	assert.NotContains(t, content, `"`+glog.KeyTraceID+`"`)
	assert.NotContains(t, content, `"`+glog.KeySpanID+`"`)
	assert.NotContains(t, content, `"`+glog.KeyTraceFlags+`"`)
}

func TestSlogLoggerRotation(t *testing.T) {
	tempDir := "log/slog-rotation-test"
	defer os.RemoveAll(tempDir)

	config := &glog.LogConfig{
		Service:    "slog-rotation-test",
		Level:      glog.InfoLevel,
		Writers:    []glog.WriterConfig{{Type: glog.WriterFile, Dir: tempDir, MaxSize: 1, MaxBackups: 5, MaxAge: 7, Compress: false}},
		LoggerType: glog.LoggerTypeSlog,
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
	baseFile := filepath.Join(expectedDir, "slog-rotation-test_full.log")

	assert.True(t, glog.FileExists(baseFile), "Current log file should exist")

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

	glog.Close()
}

func TestSlogMultiWritersFileAndConsole(t *testing.T) {
	tempDir := t.TempDir()
	config := &glog.LogConfig{
		Service:    "slog-multi",
		Module:     "test",
		Level:      glog.InfoLevel,
		LoggerType: glog.LoggerTypeSlog,
		Writers: []glog.WriterConfig{
			{Type: glog.WriterConsole, Level: glog.DebugLevel},
			{Type: glog.WriterFile, Dir: tempDir, MaxSize: 100, MaxBackups: 3, MaxAge: 7},
		},
	}
	logger, err := glog.NewLogger(config)
	assert.Nil(t, err)
	defer logger.Close()
	ctx := context.Background()
	logger.Info(ctx, "multi writer test")
	expectedDir := filepath.Join(tempDir, time.Now().Format("20060102"))
	expectedFile := filepath.Join(expectedDir, "slog-multi_full.log")
	assert.True(t, glog.FileExists(expectedFile), expectedFile)
}

func TestSlogMultiWritersLevelSplit(t *testing.T) {
	tempDir := t.TempDir()
	config := &glog.LogConfig{
		Service:    "slog-split",
		Module:     "test",
		Level:      glog.InfoLevel,
		LoggerType: glog.LoggerTypeSlog,
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
	fullFile := filepath.Join(tempDir, dateStr, "slog-split_full.log")
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

func TestSlogWfOnlyNoExtraFullFile(t *testing.T) {
	tempDir := t.TempDir()
	config := &glog.LogConfig{
		Service:    "slog-wfonly",
		Module:     "test",
		Level:      glog.InfoLevel,
		LoggerType: glog.LoggerTypeSlog,
		Writers: []glog.WriterConfig{
			{Type: glog.WriterFile, Dir: tempDir, WfOnly: true},
		},
	}
	logger, err := glog.NewLogger(config)
	assert.Nil(t, err)
	logger.Info(context.Background(), "info msg")
	logger.Error(context.Background(), "error msg")
	logger.Close()
	dateStr := time.Now().Format("20060102")
	assert.True(t, glog.FileExists(filepath.Join(tempDir, dateStr, "slog-wfonly_wf.log")))
	assert.False(t, glog.FileExists(filepath.Join(tempDir, dateStr, "slog-wfonly_full.log")))
	wf, _ := os.ReadFile(filepath.Join(tempDir, dateStr, "slog-wfonly_wf.log"))
	assert.NotContains(t, string(wf), "info msg")
	assert.Contains(t, string(wf), "error msg")
}

func TestSlogWriterConfigDefaults(t *testing.T) {
	wc := glog.WriterConfig{Type: glog.WriterFile}
	assert.Equal(t, "./logs", wc.EffectiveDir())
	assert.Equal(t, "myservice.log", wc.EffectiveFileName("myservice"))
	custom := glog.WriterConfig{Type: glog.WriterFile, FileName: "custom.log"}
	assert.Equal(t, "custom.log", custom.EffectiveFileName("myservice"))
	ms, mb, ma := wc.EffectiveRotateConfig()
	assert.Equal(t, 100, ms)
	assert.Equal(t, 10, mb)
	assert.Equal(t, 7, ma)
}
