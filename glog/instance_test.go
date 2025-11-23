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
)

func TestDefaultLogger(t *testing.T) {
	ctx := context.Background()
	Debug(ctx, "message", "debug")
	Info(ctx, "message", "info")
	Warn(ctx, "message", "warn")
	Error(ctx, "message", "error")
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic after Panic")
		}
	}()
	Panic(ctx, "message", "fatal")
}

func TestLogLevels(t *testing.T) {
	ctx := context.Background()
	Debug(ctx, "message", "debug message")
	Info(ctx, "message", "info message")
	Warn(ctx, "message", "warn message")
	Error(ctx, "message", "error message")
}

func TestInit(t *testing.T) {
	// 创建测试目录
	tempDir := "log/glog-test"
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	// defer os.RemoveAll(tempDir)

	t.Run("TestBasicInit", func(t *testing.T) {
		config := &LogConfig{
			Service: "test-service",
			Module:  "test-module",
			Level:   InfoLevel,
			Writer:  WriterFile,
			Dir:     tempDir,
		}

		// 初始化日志系统
		err := InitLogger(config)
		assert.Nil(t, err)

		// 写入一条日志
		Info(context.Background(), "test message")

		// 验证日志文件是否创建
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

		// 验证 console logger
		logger, getLoggerErr := NewLogger(config)
		assert.Nil(t, getLoggerErr)
		if logger == nil {
			t.Error("Console logger not initialized")
		}

		// 写入日志（这里主要测试不会panic）
		ctx := context.Background()
		logger.Debug(ctx, "debug to console")
		logger.Info(ctx, "info to console")
	})
}

func TestClose(t *testing.T) {
	// 测试Close函数
	Close()

	// 测试Close后是否还能使用logger
	ctx := context.Background()
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic after Close")
		}
	}()
	Info(ctx, "message", "this should panic")
}

// TestFieldHook 测试字段钩子函数
func TestHook(t *testing.T) {
	// 创建一个临时目录用于测试
	tempDir := "log/glog-test"
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// 设置测试配置
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
		// 处理消息中的密码
		if strings.Contains(message, "password") {
			re := regexp.MustCompile(`password=[^&\s]+`)
			return re.ReplaceAllString(message, "password=***")
		}
		return message
	}

	// 初始化日志器
	t.Log("Initializing logger with field hook")
	InitLogger(config, WithFieldHookFunc(phoneDesensitizationHook), WithMessageHookFunc(pwdDesensitizationHook))

	// 测试电话号码脱敏
	ctx := context.Background()
	t.Log("Logging message with phone number")
	Infow(ctx, "test message", "phone", "13812345678")

	// 测试密码脱敏
	t.Log("Logging message with password")
	Info(ctx, "test message with password=123456")
}

// TestExtraKeys 测试从上下文中提取额外字段的功能
func TestExtraKeys(t *testing.T) {
	// 创建一个临时目录用于测试
	tempDir := "log/glog-test"
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// 设置测试配置
	config := &LogConfig{
		Service:   "test",
		Module:    "test",
		Level:     DebugLevel,
		Writer:    WriterConsole,
		Dir:       tempDir,
		ExtraKeys: []string{"trace_id", "user_id", "request_id"},
	}

	// 初始化日志器
	t.Log("Initializing logger with extra keys")

	// 获取模块级别的 logger
	logger, getLoggerErr := NewLogger(config)
	if getLoggerErr != nil {
		t.Fatalf("failed to get logger: %v", getLoggerErr)
	}

	// 创建带有额外字段的上下文
	ctx := context.Background()
	ctx = context.WithValue(ctx, "trace_id", "123456")
	ctx = context.WithValue(ctx, "user_id", "user123")
	ctx = context.WithValue(ctx, "request_id", "req789")
	// 添加一个不在 ExtraKeys 中的字段，用于测试过滤
	ctx = context.WithValue(ctx, "other_field", "should_not_appear")

	// 记录一条日志
	t.Log("Logging message with extra fields")
	logger.Infow(ctx, "test message with extra fields", "key", "value")

	// 同步日志
	Close()
}

func TestLogWithFields(t *testing.T) {
	ctx := context.Background()
	Infow(ctx, "info with fields", "key1", "value1", "key2", "value2")
	Errorw(ctx, "error with fields", "error", "something went wrong", "code", 500)
}

func TestLogFormat(t *testing.T) {
	ctx := context.Background()
	Debugf(ctx, "debug format: %s", "value")
	Infof(ctx, "info format: %s", "value")
	Warnf(ctx, "warn format: %s", "value")
	Errorf(ctx, "error format: %s", "value")
}

// TestLogRotation 测试日志切割功能
func TestLogRotation(t *testing.T) {
	tempDir := "log/glog-rotation-test"
	defer os.RemoveAll(tempDir)

	// 设置较小的 MaxSize 以便触发轮转
	config := &LogConfig{
		Service:    "rotation-test",
		Level:      InfoLevel,
		Writer:     WriterFile,
		Dir:        tempDir,
		MaxSize:    1, // 1MB，便于触发轮转
		MaxBackups: 5,
		MaxAge:     7,
		Compress:   false,
	}

	err := InitLogger(config)
	assert.Nil(t, err)

	ctx := context.Background()

	// 写入大量数据以触发日志切割
	largeMessage := strings.Repeat("x", 200*1024) // 每条消息 200KB
	for i := 0; i < 10; i++ {
		Info(ctx, fmt.Sprintf("large message %d: %s", i, largeMessage))
	}

	// 等待缓冲区刷新和日志切割
	time.Sleep(2 * time.Second)

	expectedDir := filepath.Join(tempDir, time.Now().Format("20060102"))
	baseFile := filepath.Join(expectedDir, "rotation-test_full.log")

	// 验证当前日志文件存在
	assert.True(t, fileExists(baseFile), "Current log file should exist")

	// 检查是否有轮转后的文件（文件名包含时间戳）
	files, err := os.ReadDir(expectedDir)
	assert.Nil(t, err)

	// 查找轮转后的文件（格式：rotation-test_full-时间戳.log）
	rotated := false
	for _, file := range files {
		if strings.Contains(file.Name(), "rotation-test_full-") && strings.HasSuffix(file.Name(), ".log") {
			rotated = true
			break
		}
	}

	// 验证是否发生了日志切割
	assert.True(t, rotated, "Log rotation should occur when file size exceeds MaxSize")

	Close()
}
