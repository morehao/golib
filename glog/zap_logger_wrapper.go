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

	// 根据 RotateUnit 确定日志文件名的时间格式
	var timeFormat string
	switch cfg.RotateUnit {
	case RotateUnitHour:
		timeFormat = "15" // 只包含小时
	default:
		timeFormat = "" // 不包含时间
	}

	// 构建日志文件名
	var logFilename string
	if timeFormat != "" {
		logFilename = fmt.Sprintf("%s_%s_%s.log", cfg.Service, fileSuffix, time.Now().Format(timeFormat))
	} else {
		logFilename = fmt.Sprintf("%s_%s.log", cfg.Service, fileSuffix)
	}

	logFilepath := path.Join(dir, logFilename)

	// 打开日志文件
	file, openErr := os.OpenFile(logFilepath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if openErr != nil {
		return nil, openErr
	}

	// 创建带缓冲的写入器
	writer := &zapcore.BufferedWriteSyncer{
		WS:            zapcore.AddSync(file),
		Size:          256 * 1024,
		FlushInterval: time.Second * 5,
		Clock:         nil,
	}

	return writer, nil
}
