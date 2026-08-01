package slog

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/morehao/golib/glog"
	"gopkg.in/natefinch/lumberjack.v2"
)

type gSlogHandler struct {
	handler         slog.Handler
	messageHookFunc glog.MessageHookFunc
}

func replaceAttr(groups []string, a slog.Attr) slog.Attr {
	if len(groups) > 0 {
		return a
	}
	switch a.Key {
	case slog.TimeKey:
		if t, ok := a.Value.Any().(time.Time); ok {
			return slog.String(slog.TimeKey, t.Format("2006-01-02 15:04:05.000000"))
		}
	case slog.LevelKey:
		level, ok := a.Value.Any().(slog.Level)
		if !ok {
			return a
		}
		switch level {
		case slogLevelPanic:
			return slog.String(slog.LevelKey, "panic")
		case slogLevelFatal:
			return slog.String(slog.LevelKey, "fatal")
		}
		return slog.String(slog.LevelKey, level.String())
	case slog.SourceKey:
		src, ok := a.Value.Any().(*slog.Source)
		if !ok || src == nil {
			return slog.Attr{}
		}
		return slog.String("caller", trimSourceFile(src.File)+":"+strconv.Itoa(src.Line))
	}
	return a
}

func trimSourceFile(absPath string) string {
	if idx := strings.Index(absPath, "/pkg/mod/"); idx != -1 {
		return absPath[idx+len("/pkg/mod/"):]
	}
	dir, file := path.Split(absPath)
	if dir == "" {
		return absPath
	}
	dir = dir[:len(dir)-1]
	parent := path.Base(dir)
	return parent + "/" + file
}

func (h *gSlogHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.handler.Enabled(ctx, level)
}

func (h *gSlogHandler) Handle(ctx context.Context, r slog.Record) error {
	if glog.SkipLog(ctx) {
		return nil
	}
	if h.messageHookFunc != nil {
		r = r.Clone()
		r.Message = h.messageHookFunc(r.Message)
	}
	return h.handler.Handle(ctx, r)
}

func (h *gSlogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &gSlogHandler{
		handler:         h.handler.WithAttrs(attrs),
		messageHookFunc: h.messageHookFunc,
	}
}

func (h *gSlogHandler) WithGroup(name string) slog.Handler {
	return &gSlogHandler{
		handler:         h.handler.WithGroup(name),
		messageHookFunc: h.messageHookFunc,
	}
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

type gSlogFileWriter struct {
	wc           glog.WriterConfig
	serviceName  string
	suffix       string // "_full" 或 "_wf"
	rotateMu     sync.Mutex
	current      atomic.Pointer[lumberjack.Logger]
	today        string
	nextRotateAt atomic.Int64
}

func newSlogFileWriter(wc glog.WriterConfig, serviceName, suffix string) (*gSlogFileWriter, error) {
	w := &gSlogFileWriter{wc: wc, serviceName: serviceName, suffix: suffix}
	if err := w.rotate(true); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *gSlogFileWriter) filePath(now time.Time) string {
	dateStr := now.Format("20060102")
	dir := path.Join(w.wc.EffectiveDir(), dateStr)
	baseName := w.wc.EffectiveFileName(w.serviceName)
	ext := path.Ext(baseName)
	nameWithoutExt := baseName[:len(baseName)-len(ext)]
	return path.Join(dir, nameWithoutExt+w.suffix+ext)
}

func (w *gSlogFileWriter) needsRotate(now time.Time) bool {
	return now.Unix() >= w.nextRotateAt.Load()
}

func (w *gSlogFileWriter) rotate(force bool) error {
	w.rotateMu.Lock()
	defer w.rotateMu.Unlock()

	now := time.Now()
	tomorrow := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())

	if !force && !w.needsRotate(now) {
		return nil
	}
	newToday := now.Format("20060102")
	if lj := w.current.Load(); lj != nil && w.today == newToday {
		w.nextRotateAt.Store(tomorrow.Unix())
		return nil
	}

	filePath := w.filePath(now)
	if err := os.MkdirAll(path.Dir(filePath), os.ModePerm); err != nil {
		return fmt.Errorf("glog: mkdir %s: %w", path.Dir(filePath), err)
	}
	maxSize, maxBackups, maxAge := w.wc.EffectiveRotateConfig()
	lj := &lumberjack.Logger{
		Filename:   filePath,
		MaxSize:    maxSize,
		MaxBackups: maxBackups,
		MaxAge:     maxAge,
		Compress:   w.wc.Compress,
		LocalTime:  true,
	}

	old := w.current.Swap(lj)
	w.today = newToday
	w.nextRotateAt.Store(tomorrow.Unix())

	if old != nil {
		go func() { _ = old.Close() }()
	}
	return nil
}

func (w *gSlogFileWriter) Write(p []byte) (int, error) {
	if w.needsRotate(time.Now()) {
		if err := w.rotate(false); err != nil {
			return 0, err
		}
	}
	lj := w.current.Load()
	return lj.Write(p)
}

func (w *gSlogFileWriter) Close() error {
	w.rotateMu.Lock()
	lj := w.current.Load()
	w.rotateMu.Unlock()
	if lj != nil {
		return lj.Close()
	}
	return nil
}

type multiHandler struct {
	handlers []slog.Handler
}

func newMultiHandler(handlers ...slog.Handler) *multiHandler {
	return &multiHandler{handlers: handlers}
}

func (h *multiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, handler := range h.handlers {
		if handler.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (h *multiHandler) Handle(ctx context.Context, r slog.Record) error {
	var firstErr error
	for _, handler := range h.handlers {
		if handler.Enabled(ctx, r.Level) {
			rr := r.Clone()
			if err := handler.Handle(ctx, rr); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

func (h *multiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	handlers := make([]slog.Handler, len(h.handlers))
	for i, handler := range h.handlers {
		handlers[i] = handler.WithAttrs(attrs)
	}
	return &multiHandler{handlers: handlers}
}

func (h *multiHandler) WithGroup(name string) slog.Handler {
	handlers := make([]slog.Handler, len(h.handlers))
	for i, handler := range h.handlers {
		handlers[i] = handler.WithGroup(name)
	}
	return &multiHandler{handlers: handlers}
}
