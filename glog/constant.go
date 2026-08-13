package glog

type LoggerType string

const (
	LoggerTypeZap  LoggerType = "zap"
	LoggerTypeSlog LoggerType = "slog"
)

type LoggerFactory func(cfg *LogConfig, opts ...Option) (Logger, error)

type Level string

const (
	DebugLevel Level = "debug"
	InfoLevel  Level = "info"
	WarnLevel  Level = "warn"
	ErrorLevel Level = "error"
	PanicLevel Level = "panic"
	FatalLevel Level = "fatal"
)

type WriterType string

const (
	WriterConsole WriterType = "console"
	WriterFile    WriterType = "file"
)

const (
	DefaultServiceName = "app"
	DefaultModuleName  = "default"
)
