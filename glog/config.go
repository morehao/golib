/*
 * @Author: morehao morehao@qq.com
 * @Date: 2025-04-26 09:55:22
 * @LastEditors: morehao morehao@qq.com
 * @LastEditTime: 2025-04-26 16:50:59
 * @FilePath: /golib/glog/config.go
 * @Description: 这是默认设置,请设置`customMade`, 打开koroFileHeader查看配置 进行设置: https://github.com/OBKoro1/koro1FileHeader/wiki/%E9%85%8D%E7%BD%AE
 */
package glog

type LogConfig struct {
	Service         string
	Module          string
	Level           Level      `json:"level" yaml:"level"`
	Writer          WriterType `json:"writer" yaml:"writer"`
	Dir             string     `json:"dir" yaml:"dir"`
	ExtraKeys       []string   `json:"extra_keys" yaml:"extra_keys"`
	MaxSize         int        `json:"max_size" yaml:"max_size"`
	MaxBackups      int        `json:"max_backups" yaml:"max_backups"`
	MaxAge          int        `json:"max_age" yaml:"max_age"`
	Compress        bool       `json:"compress" yaml:"compress"`
	EnableOTELTrace bool       `json:"enable_otel_trace" yaml:"enable_otel_trace"`
	LoggerType      LoggerType `json:"logger_type" yaml:"logger_type"`
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
		Writer:          WriterConsole,
		Dir:             DefaultLogDir,
		MaxSize:         100,
		MaxBackups:      10,
		MaxAge:          7,
		Compress:        false,
		EnableOTELTrace: true,
		LoggerType:      LoggerTypeSlog,
	}
}
