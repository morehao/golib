package glog

import (
	"fmt"
	"os"
	"path"
	"strings"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/buffer"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

type gZapEncoder struct {
	zapcore.Encoder
	fieldHookFunc   FieldHookFunc
	messageHookFunc MessageHookFunc
}

func getZapEncoder(cfg *zapLoggerConfig) zapcore.Encoder {
	encoderCfg := zap.NewProductionEncoderConfig()
	encoderCfg.NameKey = "module"
	// encoderCfg.EncodeTime 设置为本地时间到纳秒
	encoderCfg.EncodeTime = func(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
		enc.AppendString(t.Format("2006-01-02 15:04:05.000000"))
	}

	encoder := zapcore.NewJSONEncoder(encoderCfg)
	customEncoder := &gZapEncoder{
		Encoder: encoder,
	}
	// 如果配置了字段钩子函数或消息钩子函数，则使用自定义编码器
	if cfg != nil {
		customEncoder.fieldHookFunc = cfg.fieldHookFunc
		customEncoder.messageHookFunc = cfg.messageHookFunc
	}

	return customEncoder
}

func (enc *gZapEncoder) Clone() zapcore.Encoder {
	encoderClone := enc.Encoder.Clone()
	return &gZapEncoder{
		Encoder:         encoderClone,
		fieldHookFunc:   enc.fieldHookFunc,
		messageHookFunc: enc.messageHookFunc,
	}
}

func (enc *gZapEncoder) EncodeEntry(ent zapcore.Entry, fields []zapcore.Field) (*buffer.Buffer, error) {

	// 执行字段钩子函数
	if enc.fieldHookFunc != nil {
		kvs := make([]Field, 0, len(fields))
		for _, f := range fields {
			kvs = append(kvs, KV(f.Key, f.String))
		}
		enc.fieldHookFunc(kvs)
		for i, kv := range kvs {
			fields[i].Type = zapcore.ReflectType
			fields[i].Interface = kv.Value
		}
	}

	// 执行消息钩子函数
	if enc.messageHookFunc != nil {
		ent.Message = enc.messageHookFunc(ent.Message)
	}

	// 使用修改后的字段进行编码
	return enc.Encoder.EncodeEntry(ent, fields)
}

func getZapStandoutWriter() zapcore.WriteSyncer {
	return os.Stdout
}

func getZapFileWriter(cfg *LogConfig, fileSuffix string) (zapcore.WriteSyncer, error) {
	// 目录始终按天组织
	dir := strings.TrimSuffix(cfg.Dir, "/") + "/" + time.Now().Format("20060102")
	if ok := fileExists(dir); !ok {
		_ = os.MkdirAll(dir, os.ModePerm)
	}

	// 构建日志文件名（固定格式，由 lumberjack 处理切割）
	logFilename := fmt.Sprintf("%s_%s.log", cfg.Service, fileSuffix)
	logFilepath := path.Join(dir, logFilename)

	// 设置 lumberjack 的默认值
	maxSize := cfg.MaxSize
	if maxSize <= 0 {
		maxSize = 100 // 默认 100MB
	}
	maxBackups := cfg.MaxBackups
	if maxBackups <= 0 {
		maxBackups = 10 // 默认保留 10 个文件
	}
	maxAge := cfg.MaxAge
	if maxAge <= 0 {
		maxAge = 7 // 默认保留 7 天
	}

	// 使用 lumberjack 实现日志切割和清理
	lumberjackLogger := &lumberjack.Logger{
		Filename:   logFilepath,
		MaxSize:    maxSize,    // 单个日志文件的最大大小（MB）
		MaxBackups: maxBackups, // 保留的旧日志文件数量
		MaxAge:     maxAge,     // 保留日志文件的最大天数
		Compress:   cfg.Compress, // 是否压缩旧日志文件
		LocalTime:  true,         // 使用本地时间
	}

	// 创建带缓冲的写入器
	writer := &zapcore.BufferedWriteSyncer{
		WS:            zapcore.AddSync(lumberjackLogger),
		Size:          256 * 1024,
		FlushInterval: time.Second * 5,
		Clock:         nil,
	}

	return writer, nil
}
