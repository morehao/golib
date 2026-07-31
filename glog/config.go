package glog

type LogConfig struct {
	Service         string         `json:"service" yaml:"service"`
	Module          string         `json:"module" yaml:"module"`
	Level           Level          `json:"level" yaml:"level"`
	Writers         []WriterConfig `json:"writers" yaml:"writers"`
	ExtraKeys       []string       `json:"extra_keys" yaml:"extra_keys"`
	EnableOTELTrace bool           `json:"enable_otel_trace" yaml:"enable_otel_trace"`
	LoggerType      LoggerType     `json:"logger_type" yaml:"logger_type"`
}

type WriterConfig struct {
	Type       WriterType `json:"type" yaml:"type"`
	Level      Level      `json:"level" yaml:"level"`
	FileName   string     `json:"file_name" yaml:"file_name"`
	Dir        string     `json:"dir" yaml:"dir"`
	MaxSize    int        `json:"max_size" yaml:"max_size"`
	MaxBackups int        `json:"max_backups" yaml:"max_backups"`
	MaxAge     int        `json:"max_age" yaml:"max_age"`
	Compress   bool       `json:"compress" yaml:"compress"`
	WfOnly     bool       `json:"wf_only" yaml:"wf_only"`
}

func (wc *WriterConfig) EffectiveLevel(globalLevel Level) Level {
	if wc.Level == "" {
		return globalLevel
	}
	return wc.Level
}

func (wc *WriterConfig) EffectiveDir() string {
	if wc.Dir == "" {
		return "./logs"
	}
	return wc.Dir
}

func (wc *WriterConfig) EffectiveFileName(service string) string {
	if wc.FileName != "" {
		return wc.FileName
	}
	if service == "" {
		service = DefaultServiceName
	}
	return service + ".log"
}

func (wc *WriterConfig) EffectiveRotateConfig() (maxSize, maxBackups, maxAge int) {
	maxSize = wc.MaxSize
	if maxSize <= 0 {
		maxSize = 100
	}
	maxBackups = wc.MaxBackups
	if maxBackups <= 0 {
		maxBackups = 10
	}
	maxAge = wc.MaxAge
	if maxAge <= 0 {
		maxAge = 7
	}
	return
}

func AppendExtraKeys(cfg *LogConfig, keys ...string) {
	for _, key := range keys {
		exists := false
		for _, ek := range cfg.ExtraKeys {
			if ek == key {
				exists = true
				break
			}
		}
		if !exists {
			cfg.ExtraKeys = append(cfg.ExtraKeys, key)
		}
	}
}

func GetDefaultLogConfig() *LogConfig {
	return &LogConfig{
		Service:         DefaultServiceName,
		Module:          DefaultModuleName,
		Level:           DebugLevel,
		Writers: []WriterConfig{
			{Type: WriterConsole, Level: DebugLevel},
		},
		EnableOTELTrace: true,
		LoggerType:      LoggerTypeSlog,
	}
}
