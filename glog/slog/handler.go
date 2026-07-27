package slog

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

	"github.com/morehao/golib/glog"
	"go.opentelemetry.io/otel/trace"
	"gopkg.in/natefinch/lumberjack.v2"
)

type gSlogHandler struct {
	handler         slog.Handler
	fieldHookFunc   glog.FieldHookFunc
	messageHookFunc glog.MessageHookFunc
	enableOTELTrace bool
	cfg             *glog.LogConfig
}

func newSlogHandler(cfg *glog.LogConfig, optCfg *glog.OptConfig, writer io.Writer) *gSlogHandler {
	h := &gSlogHandler{
		enableOTELTrace: cfg.EnableOTELTrace,
		cfg:             cfg,
	}

	if optCfg != nil {
		h.fieldHookFunc = optCfg.FieldHookFunc
		h.messageHookFunc = optCfg.MessageHookFunc
		if optCfg.EnableOTELTrace != nil {
			h.enableOTELTrace = *optCfg.EnableOTELTrace
		}
	}

	handlerOpts := &slog.HandlerOptions{
		AddSource:   true,
		Level:       logLevelToSlog(cfg.Level),
		ReplaceAttr: replaceLevel,
	}
	h.handler = slog.NewJSONHandler(writer, handlerOpts)
	return h
}

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
	if glog.SkipLog(ctx) {
		return nil
	}

	r = r.Clone()

	if h.messageHookFunc != nil {
		r.Message = h.messageHookFunc(r.Message)
	}

	fields := acquireFields()
	defer releaseFields(fields)

	fields = h.extractFields(ctx, fields)

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

func (h *gSlogHandler) extractFields(ctx context.Context, dst []glog.Field) []glog.Field {
	if h.enableOTELTrace {
		sc := trace.SpanFromContext(ctx).SpanContext()
		if sc.IsValid() {
			dst = append(dst,
				glog.Field{Key: glog.KeyTraceID, Value: sc.TraceID().String()},
				glog.Field{Key: glog.KeySpanID, Value: sc.SpanID().String()},
				glog.Field{Key: glog.KeyTraceFlags, Value: sc.TraceFlags().String()},
			)
		}
	}

	if h.cfg != nil {
		for _, key := range h.cfg.ExtraKeys {
			if h.enableOTELTrace && isOTELKey(key) {
				continue
			}
			if v := ctx.Value(key); v != nil {
				dst = append(dst, glog.Field{Key: key, Value: v})
			}
		}
	}

	return dst
}

func isOTELKey(key string) bool {
	return key == glog.KeyTraceID || key == glog.KeySpanID || key == glog.KeyTraceFlags
}

var fieldsPool = sync.Pool{
	New: func() any {
		s := make([]glog.Field, 0, 8)
		return &s
	},
}

func acquireFields() []glog.Field {
	p := fieldsPool.Get().(*[]glog.Field)
	return (*p)[:0]
}

func releaseFields(fields []glog.Field) {
	if cap(fields) > 64 {
		return
	}
	p := &fields
	fieldsPool.Put(p)
}

const (
	slogLevelPanic = slog.LevelError + 1
	slogLevelFatal = slog.LevelError + 2
)

func logLevelToSlog(level glog.Level) slog.Level {
	switch level {
	case glog.DebugLevel:
		return slog.LevelDebug
	case glog.InfoLevel:
		return slog.LevelInfo
	case glog.WarnLevel:
		return slog.LevelWarn
	case glog.ErrorLevel:
		return slog.LevelError
	case glog.PanicLevel:
		return slogLevelPanic
	case glog.FatalLevel:
		return slogLevelFatal
	default:
		return slog.LevelInfo
	}
}

type levelWriter struct {
	w        io.Writer
	minLevel slog.Level
}

func (lw *levelWriter) Write(p []byte) (int, error) {
	if !lw.shouldWrite(p) {
		return len(p), nil
	}
	return lw.w.Write(p)
}

func (lw *levelWriter) shouldWrite(p []byte) bool {
	scanRange := p
	if len(p) > 256 {
		scanRange = p[:256]
	}

	needle := []byte(`"level":"`)
	idx := bytes.Index(scanRange, needle)
	if idx < 0 {
		return true
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
		return true
	}
}

type writerPair struct {
	full     *lumberjack.Logger
	wf       *levelWriter
	fullFile *os.File
	wfFile   *os.File
}

type gSlogFileWriter struct {
	cfg          *glog.LogConfig
	rotateMu     sync.Mutex
	current      atomic.Pointer[writerPair]
	currentDate  atomic.Value
	nextRotateAt atomic.Int64
}

func newSlogFileWriter(cfg *glog.LogConfig) (*gSlogFileWriter, error) {
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

func (w *gSlogFileWriter) needsRotate(now time.Time) bool {
	return now.Unix() >= w.nextRotateAt.Load()
}

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

func openLogFile(filePath string) (*os.File, error) {
	f, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("glog: open %s: %w", filePath, err)
	}
	return f, nil
}

func (w *gSlogFileWriter) rotate() error {
	w.rotateMu.Lock()
	defer w.rotateMu.Unlock()

	now := time.Now()

	if !w.needsRotate(now) {
		return nil
	}

	newDateStr := now.Format("20060102")
	if cur, ok := w.currentDate.Load().(string); ok && cur == newDateStr {
		tomorrow := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
		w.nextRotateAt.Store(tomorrow.Unix())
		return nil
	}

	pair, dateStr, nextAt, err := w.buildWriterPair(now)
	if err != nil {
		return err
	}

	old := w.current.Swap(pair)
	w.currentDate.Store(dateStr)
	w.nextRotateAt.Store(nextAt)

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
	if w.needsRotate(time.Now()) {
		if err := w.rotate(); err != nil {
			return 0, err
		}
	}

	pair := w.current.Load()

	n, err := pair.full.Write(p)
	if err != nil {
		return n, err
	}

	_, _ = pair.wf.Write(p)

	return n, nil
}

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
