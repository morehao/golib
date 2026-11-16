package zap

import (
	"fmt"
	"os"
	"path"
	"time"

	"github.com/morehao/golib/glog"
	"go.uber.org/zap"
	"go.uber.org/zap/buffer"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

type gZapEncoder struct {
	zapcore.Encoder
	messageHookFunc glog.MessageHookFunc
}

func getZapEncoder(cfg *zapLoggerConfig) zapcore.Encoder {
	encoderCfg := zap.NewProductionEncoderConfig()
	encoderCfg.NameKey = "module"
	encoderCfg.EncodeTime = func(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
		enc.AppendString(t.Format("2006-01-02 15:04:05.000000"))
	}

	encoder := zapcore.NewJSONEncoder(encoderCfg)
	customEncoder := &gZapEncoder{
		Encoder: encoder,
	}
	if cfg != nil {
		customEncoder.messageHookFunc = cfg.messageHookFunc
	}
	return customEncoder
}

func (enc *gZapEncoder) Clone() zapcore.Encoder {
	return &gZapEncoder{
		Encoder:         enc.Encoder.Clone(),
		messageHookFunc: enc.messageHookFunc,
	}
}

func (enc *gZapEncoder) EncodeEntry(ent zapcore.Entry, fields []zapcore.Field) (*buffer.Buffer, error) {
	if enc.messageHookFunc != nil {
		ent.Message = enc.messageHookFunc(ent.Message)
	}
	return enc.Encoder.EncodeEntry(ent, fields)
}

func getZapStandoutWriter() zapcore.WriteSyncer {
	return os.Stdout
}

// fileWriter 是 zap 的文件写入器：单个文件 + lumberjack 按大小/份数/天数滚动。
// 不再按天分目录，日志统一写到 <Dir>/<name><suffix>.log，交由 lumberjack 清理旧文件。
type fileWriter struct {
	buffered *zapcore.BufferedWriteSyncer
}

func newFileWriter(wc glog.WriterConfig, serviceName, suffix string) (*fileWriter, error) {
	dir := wc.EffectiveDir()
	if err := os.MkdirAll(dir, os.ModePerm); err != nil {
		return nil, fmt.Errorf("glog: mkdir %s: %w", dir, err)
	}

	baseName := wc.EffectiveFileName(serviceName)
	ext := path.Ext(baseName)
	nameWithoutExt := baseName[:len(baseName)-len(ext)]
	logFilepath := path.Join(dir, nameWithoutExt+suffix+ext)

	maxSize, maxBackups, maxAge := wc.EffectiveRotateConfig()

	lj := &lumberjack.Logger{
		Filename:   logFilepath,
		MaxSize:    maxSize,
		MaxBackups: maxBackups,
		MaxAge:     maxAge,
		Compress:   wc.Compress,
		LocalTime:  true,
	}

	return &fileWriter{
		buffered: &zapcore.BufferedWriteSyncer{
			WS:            zapcore.AddSync(lj),
			Size:          256 * 1024,
			FlushInterval: 5 * time.Second,
		},
	}, nil
}

func (w *fileWriter) Write(p []byte) (n int, err error) {
	return w.buffered.Write(p)
}

func (w *fileWriter) Sync() error {
	return w.buffered.Sync()
}

// Close 停止并刷出缓冲写，释放后台 goroutine 与底层文件资源。
func (w *fileWriter) Close() error {
	if w.buffered != nil {
		return w.buffered.Stop()
	}
	return nil
}


