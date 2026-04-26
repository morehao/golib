package glog

import (
	"fmt"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/buffer"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

// ---------------------------------------------------------------------------
// Encoder
// ---------------------------------------------------------------------------

// gZapEncoder 只负责 messageHook，fieldHook 已上移到 ctxLogw 层处理。
type gZapEncoder struct {
	zapcore.Encoder
	messageHookFunc MessageHookFunc
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

// ---------------------------------------------------------------------------
// Console writer
// ---------------------------------------------------------------------------

func getZapStandoutWriter() zapcore.WriteSyncer {
	return os.Stdout
}

// ---------------------------------------------------------------------------
// Daily-rotate file writer
// ---------------------------------------------------------------------------

// dailyRotateWriter 在每次 Write 时检测日期，跨天后自动切换到新目录/文件。
// 同时内置 256KB 缓冲，每 5 秒强制刷盘，与原实现保持一致。
type dailyRotateWriter struct {
	mu         sync.Mutex
	cfg        *LogConfig
	fileSuffix string // "full" or "wf"

	// 当前活跃的 lumberjack logger 及其对应的日期字符串
	current *lumberjack.Logger
	today   string

	// 带缓冲的包装层，跨天时需要一并替换
	buffered *zapcore.BufferedWriteSyncer
}

func newDailyRotateWriter(cfg *LogConfig, fileSuffix string) (*dailyRotateWriter, error) {
	w := &dailyRotateWriter{
		cfg:        cfg,
		fileSuffix: fileSuffix,
	}
	// 初始化当天的 writer
	if err := w.rotate(time.Now().Format("20060102")); err != nil {
		return nil, err
	}
	return w, nil
}

// rotate 切换到新的日期目录，必须在持有 mu 或初始化阶段调用。
func (w *dailyRotateWriter) rotate(today string) error {
	// 关闭旧的缓冲 writer（刷盘）
	if w.buffered != nil {
		_ = w.buffered.Stop()
	}

	dir := strings.TrimSuffix(w.cfg.Dir, "/") + "/" + today
	if err := os.MkdirAll(dir, os.ModePerm); err != nil {
		return fmt.Errorf("glog: mkdir %s: %w", dir, err)
	}

	logFilepath := path.Join(dir, fmt.Sprintf("%s_%s.log", w.cfg.Service, w.fileSuffix))

	maxSize := w.cfg.MaxSize
	if maxSize <= 0 {
		maxSize = 100
	}
	maxBackups := w.cfg.MaxBackups
	if maxBackups <= 0 {
		maxBackups = 10
	}
	maxAge := w.cfg.MaxAge
	if maxAge <= 0 {
		maxAge = 7
	}

	lj := &lumberjack.Logger{
		Filename:   logFilepath,
		MaxSize:    maxSize,
		MaxBackups: maxBackups,
		MaxAge:     maxAge,
		Compress:   w.cfg.Compress,
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

// Write 实现 io.Writer，跨天时自动切换。
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

// Sync 实现 zapcore.WriteSyncer。
func (w *dailyRotateWriter) Sync() error {
	w.mu.Lock()
	buf := w.buffered
	w.mu.Unlock()
	if buf != nil {
		return buf.Sync()
	}
	return nil
}

// getZapFileWriter 返回支持跨天切换的 WriteSyncer。
func getZapFileWriter(cfg *LogConfig, fileSuffix string) (zapcore.WriteSyncer, error) {
	return newDailyRotateWriter(cfg, fileSuffix)
}
