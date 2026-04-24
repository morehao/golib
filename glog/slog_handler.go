package glog

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/trace"
	"gopkg.in/natefinch/lumberjack.v2"
)

type gSlogHandler struct {
	handler         slog.Handler
	fieldHookFunc   FieldHookFunc
	messageHookFunc MessageHookFunc
	enableOTELTrace bool
	cfg             *LogConfig
}

func newSlogHandler(cfg *LogConfig, optCfg *optConfig, writer io.Writer) *gSlogHandler {
	handlerConfig := &gSlogHandler{
		enableOTELTrace: cfg.EnableOTELTrace,
		cfg:             cfg,
	}

	if optCfg != nil {
		handlerConfig.fieldHookFunc = optCfg.fieldHookFunc
		handlerConfig.messageHookFunc = optCfg.messageHookFunc
		if optCfg.enableOTELTrace != nil {
			handlerConfig.enableOTELTrace = *optCfg.enableOTELTrace
		}
	}

	handlerOpts := &slog.HandlerOptions{AddSource: true, Level: logLevelToSlog(cfg.Level)}
	handler := slog.NewJSONHandler(writer, handlerOpts)

	handlerConfig.handler = handler
	return handlerConfig
}

func (h *gSlogHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.handler.Enabled(ctx, level)
}

func (h *gSlogHandler) Handle(ctx context.Context, r slog.Record) error {
	if skipLog(ctx) {
		return nil
	}

	// Fix ⑤: Clone the record before mutating to avoid shared-slice corruption.
	r = r.Clone()

	fields := h.extractFields(ctx, r)

	if h.fieldHookFunc != nil {
		h.fieldHookFunc(fields)
	}

	msg := r.Message
	if h.messageHookFunc != nil {
		msg = h.messageHookFunc(msg)
	}
	r.Message = msg

	for _, f := range fields {
		r.Add(f.Key, f.Value)
	}

	return h.handler.Handle(ctx, r)
}

func (h *gSlogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &gSlogHandler{
		handler:         h.handler.WithAttrs(attrs),
		fieldHookFunc:   h.fieldHookFunc,
		messageHookFunc: h.messageHookFunc,
		enableOTELTrace: h.enableOTELTrace,
		cfg:             h.cfg,
	}
}

func (h *gSlogHandler) WithGroup(name string) slog.Handler {
	return &gSlogHandler{
		handler:         h.handler.WithGroup(name),
		fieldHookFunc:   h.fieldHookFunc,
		messageHookFunc: h.messageHookFunc,
		enableOTELTrace: h.enableOTELTrace,
		cfg:             h.cfg,
	}
}

func (h *gSlogHandler) extractFields(ctx context.Context, r slog.Record) []Field {
	var fields []Field
	if h.enableOTELTrace && ctx != nil {
		span := trace.SpanFromContext(ctx)
		if span != nil {
			sc := span.SpanContext()
			if sc.IsValid() {
				fields = append(fields,
					Field{Key: KeyTraceID, Value: sc.TraceID().String()},
					Field{Key: KeySpanID, Value: sc.SpanID().String()},
					Field{Key: KeyTraceFlags, Value: sc.TraceFlags().String()},
				)
			}
		}
	}

	if h.cfg != nil && len(h.cfg.ExtraKeys) > 0 {
		for _, key := range h.cfg.ExtraKeys {
			if h.enableOTELTrace && (key == KeyTraceID || key == KeySpanID || key == KeyTraceFlags) {
				continue
			}
			if v := ctx.Value(key); v != nil {
				fields = append(fields, Field{Key: key, Value: v})
			}
		}
	}

	return fields
}

func logLevelToSlog(level Level) slog.Level {
	switch level {
	case DebugLevel:
		return slog.LevelDebug
	case InfoLevel:
		return slog.LevelInfo
	case WarnLevel:
		return slog.LevelWarn
	case ErrorLevel:
		return slog.LevelError
	case PanicLevel:
		return slog.LevelError + 1
	case FatalLevel:
		return slog.LevelError + 2
	default:
		return slog.LevelInfo
	}
}

type levelWriter struct {
	w        io.Writer
	minLevel slog.Level
}

// Write 解析 JSON 中的 "level" 字段来判断是否写入。
// 若解析失败则放行，避免丢失日志。
func (lw *levelWriter) Write(p []byte) (int, error) {
	if !lw.shouldWrite(p) {
		return len(p), nil
	}
	return lw.w.Write(p)
}

func (lw *levelWriter) shouldWrite(p []byte) bool {
	// 简单的字节扫描，避免引入 encoding/json 依赖。
	// slog JSON 格式固定为 `"level":"INFO"` 这样的结构。
	needle := `"level":"`
	idx := strings.Index(string(p), needle)
	if idx < 0 {
		return true // 解析不到 level 字段时放行
	}
	rest := string(p)[idx+len(needle):]
	end := strings.IndexByte(rest, '"')
	if end < 0 {
		return true
	}
	levelStr := rest[:end]

	var level slog.Level
	if err := level.UnmarshalText([]byte(levelStr)); err != nil {
		return true // 未知 level 放行
	}
	return level >= lw.minLevel
}

type gSlogFileWriter struct {
	fullWriter  *lumberjack.Logger
	wfWriter    *lumberjack.Logger
	currentDate string
	cfg         *LogConfig
	mu          sync.Mutex
}

func newSlogFileWriter(cfg *LogConfig) (*gSlogFileWriter, error) {
	w := &gSlogFileWriter{cfg: cfg}

	if err := w.rotate(time.Now()); err != nil {
		return nil, err
	}
	return w, nil
}

// rotate 在跨天时关闭旧文件、打开新日期目录下的文件。
// 调用方须持有 mu。
func (w *gSlogFileWriter) rotate(now time.Time) error {
	dateStr := now.Format("20060102")
	if dateStr == w.currentDate {
		return nil
	}

	// 关闭旧文件
	if w.fullWriter != nil {
		_ = w.fullWriter.Close()
	}
	if w.wfWriter != nil {
		_ = w.wfWriter.Close()
	}

	dir := strings.TrimSuffix(w.cfg.Dir, "/") + "/" + dateStr
	if !fileExists(dir) {
		if err := os.MkdirAll(dir, os.ModePerm); err != nil {
			return fmt.Errorf("glog: mkdir %s: %w", dir, err)
		}
	}

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

	w.fullWriter = &lumberjack.Logger{
		Filename:   path.Join(dir, fmt.Sprintf("%s_full.log", w.cfg.Service)),
		MaxSize:    maxSize,
		MaxBackups: maxBackups,
		MaxAge:     maxAge,
		Compress:   w.cfg.Compress,
		LocalTime:  true,
	}
	w.wfWriter = &lumberjack.Logger{
		Filename:   path.Join(dir, fmt.Sprintf("%s_wf.log", w.cfg.Service)),
		MaxSize:    maxSize,
		MaxBackups: maxBackups,
		MaxAge:     maxAge,
		Compress:   w.cfg.Compress,
		LocalTime:  true,
	}
	w.currentDate = dateStr
	return nil
}

func (w *gSlogFileWriter) Write(p []byte) (n int, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Fix ③: 跨天时自动切换到新日期目录。
	if err = w.rotate(time.Now()); err != nil {
		return 0, err
	}

	n, err = w.fullWriter.Write(p)
	if err != nil {
		return n, err
	}

	// Fix ②: wf 文件只写 Warn 及以上级别。
	if w.wfWriter != nil {
		wlw := &levelWriter{w: w.wfWriter, minLevel: slog.LevelWarn}
		_, _ = wlw.Write(p)
	}

	return n, nil
}

// Fix ④: Sync 移除无意义的加锁，明确说明 lumberjack 不支持 Sync。
func (w *gSlogFileWriter) Sync() error {
	// lumberjack.Logger 不暴露 Sync/Flush 接口；
	// 每次 Write 后数据已交由 OS 缓冲，此处为满足接口契约。
	return nil
}

func (w *gSlogFileWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.fullWriter != nil {
		_ = w.fullWriter.Close()
	}
	if w.wfWriter != nil {
		_ = w.wfWriter.Close()
	}
	return nil
}
