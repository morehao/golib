package slog

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/morehao/golib/glog"
	"github.com/morehao/golib/gutil"
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
	if gutil.SkipLog(ctx) {
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

// gSlogFileWriter 是 slog 的文件写入器：单个文件 + lumberjack 按大小/份数/天数滚动。
// 不再按天分目录，日志统一写到 <Dir>/<name><suffix>.log，交由 lumberjack 清理旧文件。
type gSlogFileWriter struct {
	lj *lumberjack.Logger
}

func newSlogFileWriter(wc glog.WriterConfig, serviceName, suffix string) (*gSlogFileWriter, error) {
	dir := wc.EffectiveDir()
	if err := os.MkdirAll(dir, os.ModePerm); err != nil {
		return nil, fmt.Errorf("glog: mkdir %s: %w", dir, err)
	}

	baseName := wc.EffectiveFileName(serviceName)
	ext := path.Ext(baseName)
	nameWithoutExt := baseName[:len(baseName)-len(ext)]
	filePath := path.Join(dir, nameWithoutExt+suffix+ext)

	maxSize, maxBackups, maxAge := wc.EffectiveRotateConfig()
	lj := &lumberjack.Logger{
		Filename:   filePath,
		MaxSize:    maxSize,
		MaxBackups: maxBackups,
		MaxAge:     maxAge,
		Compress:   wc.Compress,
		LocalTime:  true,
	}
	return &gSlogFileWriter{lj: lj}, nil
}

func (w *gSlogFileWriter) Write(p []byte) (int, error) {
	return w.lj.Write(p)
}

func (w *gSlogFileWriter) Close() error {
	if w.lj != nil {
		return w.lj.Close()
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
