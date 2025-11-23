/*
 * @Author: morehao morehao@qq.com
 * @Date: 2025-04-26 09:55:22
 * @LastEditors: morehao morehao@qq.com
 * @LastEditTime: 2025-04-26 16:50:59
 * @FilePath: /golib/glog/config.go
 * @Description: 这是默认设置,请设置`customMade`, 打开koroFileHeader查看配置 进行设置: https://github.com/OBKoro1/koro1FileHeader/wiki/%E9%85%8D%E7%BD%AE
 */
package glog

// LogConfig 模块级别的日志配置
type LogConfig struct {
	// Service 服务名
	Service string
	// Module 模块名称，如 "es", "gorm", "redis" 等
	Module string
	// Level 日志级别
	Level Level `json:"level" yaml:"level"`
	// Writer 日志输出类型
	Writer WriterType `json:"writer" yaml:"writer"`
	// Dir 日志文件目录
	Dir string `json:"dir" yaml:"dir"`
	// ExtraKeys 需要从上下文中提取的额外字段
	ExtraKeys []string `json:"extra_keys" yaml:"extra_keys"`
	// MaxSize 单个日志文件的最大大小（MB），超过则切割，默认 100
	MaxSize int `json:"max_size" yaml:"max_size"`
	// MaxBackups 保留的旧日志文件数量，默认 10
	MaxBackups int `json:"max_backups" yaml:"max_backups"`
	// MaxAge 保留日志文件的最大天数，默认 7
	MaxAge int `json:"max_age" yaml:"max_age"`
	// Compress 是否压缩旧日志文件，默认 false
	Compress bool `json:"compress" yaml:"compress"`
}

func GetDefaultLogConfig() *LogConfig {
	return &LogConfig{
		Service:    defaultServiceName,
		Module:     defaultModuleName,
		Level:      DebugLevel,
		Writer:     WriterConsole,
		Dir:        defaultLogDir,
		MaxSize:    100,
		MaxBackups: 10,
		MaxAge:     7,
		Compress:   false,
	}
}
