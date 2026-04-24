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
	mu              sync.Mutex
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
	return h.handler.WithGroup(name)
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

type gSlogFileWriter struct {
	fullWriter *lumberjack.Logger
	wfWriter   *lumberjack.Logger
	mu         sync.Mutex
}

func newSlogFileWriter(cfg *LogConfig) (*gSlogFileWriter, error) {
	dir := strings.TrimSuffix(cfg.Dir, "/") + "/" + time.Now().Format("20060102")
	if ok := fileExists(dir); !ok {
		_ = os.MkdirAll(dir, os.ModePerm)
	}

	maxSize := cfg.MaxSize
	if maxSize <= 0 {
		maxSize = 100
	}
	maxBackups := cfg.MaxBackups
	if maxBackups <= 0 {
		maxBackups = 10
	}
	maxAge := cfg.MaxAge
	if maxAge <= 0 {
		maxAge = 7
	}

	fullFilename := path.Join(dir, fmt.Sprintf("%s_full.log", cfg.Service))
	fullWriter := &lumberjack.Logger{
		Filename:   fullFilename,
		MaxSize:    maxSize,
		MaxBackups: maxBackups,
		MaxAge:     maxAge,
		Compress:   cfg.Compress,
		LocalTime:  true,
	}

	wfFilename := path.Join(dir, fmt.Sprintf("%s_wf.log", cfg.Service))
	wfWriter := &lumberjack.Logger{
		Filename:   wfFilename,
		MaxSize:    maxSize,
		MaxBackups: maxBackups,
		MaxAge:     maxAge,
		Compress:   cfg.Compress,
		LocalTime:  true,
	}

	return &gSlogFileWriter{
		fullWriter: fullWriter,
		wfWriter:   wfWriter,
	}, nil
}

func (w *gSlogFileWriter) Write(p []byte) (n int, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	n, err = w.fullWriter.Write(p)
	if err != nil {
		return n, err
	}

	if w.wfWriter != nil {
		_, _ = w.wfWriter.Write(p)
	}

	return n, nil
}

func (w *gSlogFileWriter) Sync() error {
	w.mu.Lock()
	defer w.mu.Unlock()
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