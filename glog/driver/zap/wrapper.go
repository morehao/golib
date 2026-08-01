package zap

import (
	"fmt"
	"os"
	"path"
	"strings"
	"sync"
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

type dailyRotateWriter struct {
	mu          sync.Mutex
	wc          glog.WriterConfig
	serviceName string
	suffix      string // "_full" 或 "_wf"

	current  *lumberjack.Logger
	today    string
	buffered *zapcore.BufferedWriteSyncer
}

func newDailyRotateWriter(wc glog.WriterConfig, serviceName, suffix string) (*dailyRotateWriter, error) {
	w := &dailyRotateWriter{
		wc:          wc,
		serviceName: serviceName,
		suffix:      suffix,
	}
	if err := w.rotate(time.Now().Format("20060102")); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *dailyRotateWriter) rotate(today string) error {
	if w.buffered != nil {
		_ = w.buffered.Stop()
	}

	dir := strings.TrimSuffix(w.wc.EffectiveDir(), "/") + "/" + today
	if err := os.MkdirAll(dir, os.ModePerm); err != nil {
		return fmt.Errorf("glog: mkdir %s: %w", dir, err)
	}

	baseName := w.wc.EffectiveFileName(w.serviceName)
	ext := path.Ext(baseName)
	nameWithoutExt := baseName[:len(baseName)-len(ext)]
	logFilepath := path.Join(dir, nameWithoutExt+w.suffix+ext)

	maxSize, maxBackups, maxAge := w.wc.EffectiveRotateConfig()

	lj := &lumberjack.Logger{
		Filename:   logFilepath,
		MaxSize:    maxSize,
		MaxBackups: maxBackups,
		MaxAge:     maxAge,
		Compress:   w.wc.Compress,
		LocalTime:  true,
	}

	w.current = lj
	w.today = today
	w.buffered = &zapcore.BufferedWriteSyncer{
		WS:            zapcore.AddSync(lj),
		Size:          256 * 1024,
		FlushInterval: 5 * time.Second,
	}
	return nil
}

func (w *dailyRotateWriter) Write(p []byte) (n int, err error) {
	today := time.Now().Format("20060102")

	w.mu.Lock()
	if today != w.today {
		if rotateErr := w.rotate(today); rotateErr != nil {
			w.mu.Unlock()
			return 0, rotateErr
		}
	}
	buf := w.buffered
	w.mu.Unlock()

	return buf.Write(p)
}

func (w *dailyRotateWriter) Sync() error {
	w.mu.Lock()
	buf := w.buffered
	w.mu.Unlock()
	if buf != nil {
		return buf.Sync()
	}
	return nil
}

// Close 停止并刷出缓冲写，释放后台 goroutine 与底层文件资源。
func (w *dailyRotateWriter) Close() error {
	w.mu.Lock()
	buf := w.buffered
	w.buffered = nil
	w.mu.Unlock()
	if buf != nil {
		return buf.Stop()
	}
	return nil
}


