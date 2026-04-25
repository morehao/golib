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
	cfg             *LogConfig // 只读，构造后不修改
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

// replaceLevel 把内部扩展 Level 值转为语义字符串，避免输出 "ERROR+1" 之类。
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

	// 提取横切字段（OTEL trace + ctx extra keys），使用 pool 减少 GC 压力
	fields := acquireFields()
	defer releaseFields(fields)

	fields = h.extractFields(ctx, fields)

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
		cfg:             h.cfg, // cfg 构造后只读，共享指针安全
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

// extractFields 从 ctx 提取需要附加到每条日志的字段，结果追加到 dst 中。
// 只在 handler 层调用一次，不在 logger 层重复。
func (h *gSlogHandler) extractFields(ctx context.Context, dst []Field) []Field {
	if h.enableOTELTrace {
		sc := trace.SpanFromContext(ctx).SpanContext()
		if sc.IsValid() {
			dst = append(dst,
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
				dst = append(dst, Field{Key: key, Value: v})
			}
		}
	}

	return dst
}

func isOTELKey(key string) bool {
	return key == KeyTraceID || key == KeySpanID || key == KeyTraceFlags
}

// ---------------------------------------------------------------------------
// Fields Pool —— 复用 []Field，减少高频日志路径的 GC 压力
// ---------------------------------------------------------------------------

var fieldsPool = sync.Pool{
	New: func() any {
		s := make([]Field, 0, 8)
		return &s
	},
}

func acquireFields() []Field {
	p := fieldsPool.Get().(*[]Field)
	return (*p)[:0]
}

func releaseFields(fields []Field) {
	// 避免池中持有大 slice 导致内存泄漏
	if cap(fields) > 64 {
		return
	}
	p := &fields
	fieldsPool.Put(p)
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

// shouldWrite 只扫描每行日志的前 256 字节（level 字段由 slog JSON handler 固定输出在前部），
// 避免 message 或其他字段中含 `"level":"` 导致误判。
func (lw *levelWriter) shouldWrite(p []byte) bool {
	scanRange := p
	if len(p) > 256 {
		scanRange = p[:256]
	}

	needle := []byte(`"level":"`)
	idx := bytes.Index(scanRange, needle)
	if idx < 0 {
		return true // 解析不到 level 字段时放行
	}
	rest := scanRange[idx+len(needle):]
	end := bytes.IndexByte(rest, '"')
	if end < 0 {
		return true
	}
	levelBytes := rest[:end]

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

// writerPair 持有一对日志文件 writer，作为原子替换的整体单元。
type writerPair struct {
	full     *lumberjack.Logger
	wf       *levelWriter
	fullFile *os.File // 持有底层 *os.File，用于 Close 时 Sync 刷盘
	wfFile   *os.File
}

type gSlogFileWriter struct {
	cfg          *LogConfig
	rotateMu     sync.Mutex // 仅保护 rotate 操作，不参与常规写入
	current      atomic.Pointer[writerPair]
	currentDate  atomic.Value // string，存当前日期 "20060102"
	nextRotateAt atomic.Int64 // Unix seconds，下一次需要 rotate 的时间点
}

func newSlogFileWriter(cfg *LogConfig) (*gSlogFileWriter, error) {
	w := &gSlogFileWriter{cfg: cfg}
	pair, dateStr, nextAt, err := w.buildWriterPair(time.Now())
	if err != nil {
		return nil, err
	}
	w.current.Store(pair)
	w.currentDate.Store(dateStr)
	w.nextRotateAt.Store(nextAt)
	return w, nil
}

// needsRotate 通过 atomic 快速判断是否需要切换日期目录，无需加锁。
func (w *gSlogFileWriter) needsRotate(now time.Time) bool {
	return now.Unix() >= w.nextRotateAt.Load()
}

// buildWriterPair 构造新的 writerPair，不持有任何锁。
func (w *gSlogFileWriter) buildWriterPair(now time.Time) (*writerPair, string, int64, error) {
	dateStr := now.Format("20060102")
	dir := w.cfg.Dir + "/" + dateStr
	if err := os.MkdirAll(dir, os.ModePerm); err != nil {
		return nil, "", 0, fmt.Errorf("glog: mkdir %s: %w", dir, err)
	}

	maxSize, maxBackups, maxAge := w.resolvedRotateConfig()

	fullPath := path.Join(dir, fmt.Sprintf("%s_full.log", w.cfg.Service))
	wfPath := path.Join(dir, fmt.Sprintf("%s_wf.log", w.cfg.Service))

	fullLJ := &lumberjack.Logger{
		Filename:   fullPath,
		MaxSize:    maxSize,
		MaxBackups: maxBackups,
		MaxAge:     maxAge,
		Compress:   w.cfg.Compress,
		LocalTime:  true,
	}
	wfLJ := &lumberjack.Logger{
		Filename:   wfPath,
		MaxSize:    maxSize,
		MaxBackups: maxBackups,
		MaxAge:     maxAge,
		Compress:   w.cfg.Compress,
		LocalTime:  true,
	}

	// 打开底层 *os.File，仅用于 Close 时 Sync 刷盘，不参与常规写入
	fullFile, err := openLogFile(fullPath)
	if err != nil {
		return nil, "", 0, err
	}
	wfFile, err := openLogFile(wfPath)
	if err != nil {
		_ = fullFile.Close()
		return nil, "", 0, err
	}

	pair := &writerPair{
		full:     fullLJ,
		wf:       &levelWriter{w: wfLJ, minLevel: slog.LevelWarn},
		fullFile: fullFile,
		wfFile:   wfFile,
	}

	tomorrow := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
	return pair, dateStr, tomorrow.Unix(), nil
}

// openLogFile 以 append 模式打开日志文件，仅持有 fd 用于 Sync，不做日常写入。
func openLogFile(filePath string) (*os.File, error) {
	f, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("glog: open %s: %w", filePath, err)
	}
	return f, nil
}

// rotate 在持有 rotateMu 的情况下完成日期切换，采用 double-check 防止重复 rotate。
func (w *gSlogFileWriter) rotate() error {
	w.rotateMu.Lock()
	defer w.rotateMu.Unlock()

	// 重新取时间，避免加锁前后跨天导致 dateStr 不一致
	now := time.Now()

	// double-check：可能在等锁期间已被其他 goroutine rotate 完毕
	if !w.needsRotate(now) {
		return nil
	}

	newDateStr := now.Format("20060102")
	if cur, ok := w.currentDate.Load().(string); ok && cur == newDateStr {
		// 日期未变（极罕见的时钟抖动场景），仅更新 nextRotateAt 防止无限循环
		tomorrow := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
		w.nextRotateAt.Store(tomorrow.Unix())
		return nil
	}

	pair, dateStr, nextAt, err := w.buildWriterPair(now)
	if err != nil {
		return err
	}

	// 原子替换，新的 goroutine 立即可见新 pair
	old := w.current.Swap(pair)
	w.currentDate.Store(dateStr)
	w.nextRotateAt.Store(nextAt)

	// 异步关闭旧资源，不阻塞写入路径
	go closeWriterPair(old)

	return nil
}

func closeWriterPair(pair *writerPair) {
	if pair == nil {
		return
	}
	if pair.fullFile != nil {
		_ = pair.fullFile.Sync()
		_ = pair.fullFile.Close()
	}
	if pair.wfFile != nil {
		_ = pair.wfFile.Sync()
		_ = pair.wfFile.Close()
	}
	if pair.full != nil {
		_ = pair.full.Close()
	}
	if pair.wf != nil {
		if lj, ok := pair.wf.w.(*lumberjack.Logger); ok {
			_ = lj.Close()
		}
	}
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
	// 快速路径：atomic 读，无锁判断是否需要 rotate
	if w.needsRotate(time.Now()) {
		if err := w.rotate(); err != nil {
			return 0, err
		}
	}

	// 无锁读取当前 pair，lumberjack 内部自带锁保证并发安全
	pair := w.current.Load()

	n, err := pair.full.Write(p)
	if err != nil {
		return n, err
	}

	// wf 内置 levelWriter 过滤，直接写；忽略 wf 写入错误，不影响 full 路径
	_, _ = pair.wf.Write(p)

	return n, nil
}

// Close 刷盘并释放当前所有文件资源。应在服务退出时调用。
func (w *gSlogFileWriter) Close() error {
	pair := w.current.Load()
	if pair == nil {
		return nil
	}

	var firstErr error

	if pair.fullFile != nil {
		if err := pair.fullFile.Sync(); err != nil && firstErr == nil {
			firstErr = err
		}
		_ = pair.fullFile.Close()
	}
	if pair.wfFile != nil {
		if err := pair.wfFile.Sync(); err != nil && firstErr == nil {
			firstErr = err
		}
		_ = pair.wfFile.Close()
	}
	if pair.full != nil {
		_ = pair.full.Close()
	}
	if pair.wf != nil {
		if lj, ok := pair.wf.w.(*lumberjack.Logger); ok {
			_ = lj.Close()
		}
	}

	return firstErr
}
