package glog

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/trace"
	"gopkg.in/natefinch/lumberjack.v2"
)

// ---------------------------------------------------------------------------
// gSlogHandler
// ---------------------------------------------------------------------------

// gSlogHandler 是统一的 slog.Handler 实现。
// 所有横切关注点（OTEL trace、ctx extra keys、field/message hook）
// 都集中在这一层处理，logger 层不再重复注入。
type gSlogHandler struct {
	handler         slog.Handler
	fieldHookFunc   FieldHookFunc
	messageHookFunc MessageHookFunc
	enableOTELTrace bool
	cfg             *LogConfig
}

func newSlogHandler(cfg *LogConfig, optCfg *optConfig, writer io.Writer) *gSlogHandler {
	h := &gSlogHandler{
		enableOTELTrace: cfg.EnableOTELTrace,
		cfg:             cfg,
	}

	if optCfg != nil {
		h.fieldHookFunc = optCfg.fieldHookFunc
		h.messageHookFunc = optCfg.messageHookFunc
		if optCfg.enableOTELTrace != nil {
			h.enableOTELTrace = *optCfg.enableOTELTrace
		}
	}

	handlerOpts := &slog.HandlerOptions{
		AddSource: true,
		Level:     logLevelToSlog(cfg.Level),
		// 将自定义 Level 常量（PanicLevel / FatalLevel）映射为可读字符串
		ReplaceAttr: replaceLevel,
	}
	h.handler = slog.NewJSONHandler(writer, handlerOpts)
	return h
}

// replaceAttr 把内部扩展 Level 值转为语义字符串，避免输出 "ERROR+1" 之类。
func replaceLevel(groups []string, a slog.Attr) slog.Attr {
	if len(groups) != 0 || a.Key != slog.LevelKey {
		return a
	}
	level, ok := a.Value.Any().(slog.Level)
	if !ok {
		return a
	}
	switch level {
	case slogLevelPanic:
		return slog.String(slog.LevelKey, "PANIC")
	case slogLevelFatal:
		return slog.String(slog.LevelKey, "FATAL")
	}
	return a
}

func (h *gSlogHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.handler.Enabled(ctx, level)
}

func (h *gSlogHandler) Handle(ctx context.Context, r slog.Record) error {
	if skipLog(ctx) {
		return nil
	}

	// Clone 避免与调用方共享 attrs 切片
	r = r.Clone()

	// message hook
	if h.messageHookFunc != nil {
		r.Message = h.messageHookFunc(r.Message)
	}

	// 提取横切字段（OTEL trace + ctx extra keys）
	fields := h.extractFields(ctx)

	// field hook：允许外部修改/过滤字段
	if h.fieldHookFunc != nil {
		h.fieldHookFunc(fields)
	}

	for _, f := range fields {
		r.AddAttrs(slog.Any(f.Key, f.Value))
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

// extractFields 从 ctx 提取需要附加到每条日志的字段。
// 只在 handler 层调用一次，不在 logger 层重复。
func (h *gSlogHandler) extractFields(ctx context.Context) []Field {
	var fields []Field

	if h.enableOTELTrace {
		sc := trace.SpanFromContext(ctx).SpanContext()
		if sc.IsValid() {
			fields = append(fields,
				Field{Key: KeyTraceID, Value: sc.TraceID().String()},
				Field{Key: KeySpanID, Value: sc.SpanID().String()},
				Field{Key: KeyTraceFlags, Value: sc.TraceFlags().String()},
			)
		}
	}

	if h.cfg != nil {
		for _, key := range h.cfg.ExtraKeys {
			// OTEL 已注入的 key 不重复写入
			if h.enableOTELTrace && isOTELKey(key) {
				continue
			}
			if v := ctx.Value(key); v != nil {
				fields = append(fields, Field{Key: key, Value: v})
			}
		}
	}

	return fields
}

func isOTELKey(key string) bool {
	return key == KeyTraceID || key == KeySpanID || key == KeyTraceFlags
}

// ---------------------------------------------------------------------------
// Level 映射
// ---------------------------------------------------------------------------

// slog 没有内建 Panic/Fatal，用偏移量自定义，与 logLevelToSlog 保持一致。
const (
	slogLevelPanic = slog.LevelError + 1
	slogLevelFatal = slog.LevelError + 2
)

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
		return slogLevelPanic
	case FatalLevel:
		return slogLevelFatal
	default:
		return slog.LevelInfo
	}
}

// ---------------------------------------------------------------------------
// levelWriter —— wf 文件的 Warn+ 过滤层
// ---------------------------------------------------------------------------

// levelWriter 包装底层 writer，只透传 >= minLevel 的日志行。
// 实例在 gSlogFileWriter 构造时创建，之后复用，不在每次 Write 时 alloc。
type levelWriter struct {
	w        io.Writer
	minLevel slog.Level
}

func (lw *levelWriter) Write(p []byte) (int, error) {
	if !lw.shouldWrite(p) {
		return len(p), nil // 丢弃但不报错
	}
	return lw.w.Write(p)
}

// shouldWrite 用纯字节操作扫描 JSON 中的 "level" 字段，避免 string 转换和 JSON 解析开销。
func (lw *levelWriter) shouldWrite(p []byte) bool {
	needle := []byte(`"level":"`)
	idx := bytes.Index(p, needle)
	if idx < 0 {
		return true // 解析不到 level 字段时放行
	}
	rest := p[idx+len(needle):]
	end := bytes.IndexByte(rest, '"')
	if end < 0 {
		return true
	}
	levelBytes := rest[:end]

	// 直接字节比较，避免 UnmarshalText alloc 和对非标准 level 字符串的误判
	switch string(levelBytes) {
	case "DEBUG":
		return slog.LevelDebug >= lw.minLevel
	case "INFO":
		return slog.LevelInfo >= lw.minLevel
	case "WARN":
		return slog.LevelWarn >= lw.minLevel
	case "ERROR":
		return slog.LevelError >= lw.minLevel
	case "PANIC":
		return slogLevelPanic >= lw.minLevel
	case "FATAL":
		return slogLevelFatal >= lw.minLevel
	default:
		return true // 未知 level 放行
	}
}

// ---------------------------------------------------------------------------
// gSlogFileWriter —— 带日期轮转的文件 writer
// ---------------------------------------------------------------------------

type gSlogFileWriter struct {
	cfg         *LogConfig
	mu          sync.Mutex
	fullWriter  *lumberjack.Logger
	wfWriter    *levelWriter // 复用，不在 Write 时重复 alloc
	currentDate string
	// nextRotateAt 缓存当天结束时间戳，减少 time.Now()+Format 调用频率
	nextRotateAt atomic.Int64 // Unix seconds
}

func newSlogFileWriter(cfg *LogConfig) (*gSlogFileWriter, error) {
	w := &gSlogFileWriter{cfg: cfg}
	if err := w.rotateUnlocked(time.Now()); err != nil {
		return nil, err
	}
	return w, nil
}

// needsRotate 通过 atomic 快速判断是否需要切换日期目录，无需加锁。
func (w *gSlogFileWriter) needsRotate(now time.Time) bool {
	return now.Unix() >= w.nextRotateAt.Load()
}

// rotateUnlocked 切换到新日期目录；调用方须已持有 mu 或在初始化阶段。
func (w *gSlogFileWriter) rotateUnlocked(now time.Time) error {
	dateStr := now.Format("20060102")
	if dateStr == w.currentDate {
		return nil
	}

	// 关闭旧文件
	if w.fullWriter != nil {
		_ = w.fullWriter.Close()
	}
	if w.wfWriter != nil {
		if lj, ok := w.wfWriter.w.(*lumberjack.Logger); ok {
			_ = lj.Close()
		}
	}

	dir := w.cfg.Dir + "/" + dateStr
	if err := os.MkdirAll(dir, os.ModePerm); err != nil {
		return fmt.Errorf("glog: mkdir %s: %w", dir, err)
	}

	maxSize, maxBackups, maxAge := w.resolvedRotateConfig()

	fullLJ := &lumberjack.Logger{
		Filename:   path.Join(dir, fmt.Sprintf("%s_full.log", w.cfg.Service)),
		MaxSize:    maxSize,
		MaxBackups: maxBackups,
		MaxAge:     maxAge,
		Compress:   w.cfg.Compress,
		LocalTime:  true,
	}
	wfLJ := &lumberjack.Logger{
		Filename:   path.Join(dir, fmt.Sprintf("%s_wf.log", w.cfg.Service)),
		MaxSize:    maxSize,
		MaxBackups: maxBackups,
		MaxAge:     maxAge,
		Compress:   w.cfg.Compress,
		LocalTime:  true,
	}

	w.fullWriter = fullLJ
	w.wfWriter = &levelWriter{w: wfLJ, minLevel: slog.LevelWarn}
	w.currentDate = dateStr

	// 计算下一个需要 rotate 的时间点（次日 00:00:00）
	tomorrow := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
	w.nextRotateAt.Store(tomorrow.Unix())

	return nil
}

func (w *gSlogFileWriter) resolvedRotateConfig() (maxSize, maxBackups, maxAge int) {
	maxSize = w.cfg.MaxSize
	if maxSize <= 0 {
		maxSize = 100
	}
	maxBackups = w.cfg.MaxBackups
	if maxBackups <= 0 {
		maxBackups = 10
	}
	maxAge = w.cfg.MaxAge
	if maxAge <= 0 {
		maxAge = 7
	}
	return
}

func (w *gSlogFileWriter) Write(p []byte) (int, error) {
	now := time.Now()

	// 快速路径：原子读，无锁
	if w.needsRotate(now) {
		w.mu.Lock()
		// double-check，防止多个 goroutine 都通过了快速路径
		if w.needsRotate(now) {
			if err := w.rotateUnlocked(now); err != nil {
				w.mu.Unlock()
				return 0, err
			}
		}
		w.mu.Unlock()
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	n, err := w.fullWriter.Write(p)
	if err != nil {
		return n, err
	}

	// wfWriter 已内置 levelWriter，直接写；Write 内部过滤级别
	if w.wfWriter != nil {
		_, _ = w.wfWriter.Write(p)
	}

	return n, nil
}

// Sync 尽力刷盘。lumberjack 不暴露 Flush，通过反射或重新 open 均不可靠，
// 此处仅满足接口契约；Fatal 路径应通过 os.File 直写规避。
func (w *gSlogFileWriter) Sync() error {
	return nil
}

func (w *gSlogFileWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.fullWriter != nil {
		_ = w.fullWriter.Close()
	}
	if w.wfWriter != nil {
		if lj, ok := w.wfWriter.w.(*lumberjack.Logger); ok {
			_ = lj.Close()
		}
	}
	return nil
}
