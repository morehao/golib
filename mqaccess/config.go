package mqaccess

import (
	"github.com/morehao/golib/glog"
)

// Option 是各引擎子包 New/NewProducer/NewConsumer 共享的配置选项。
//
// 注意：它只承载「跨引擎一致」的配置（日志、callerSkip）。引擎专属配置
// （URL / NameServer / 凭据 / JetStream 开关等）放在各引擎自己的 Config 结构体里，
// 不塞进这里——避免顶层抽象逐渐膨胀成「最小公约数」。
type Option interface {
	apply(*Options)
}

// Options 是 Option 的聚合载体。各引擎在构造时把它并入自己的内部配置。
type Options struct {
	// loggerConfig 独立的 DB/中间件日志配置；nil 表示跟随全局 glog。
	loggerConfig *glog.LogConfig
	// callerSkip 对默认 caller 校准值的覆盖；<=0 表示用引擎默认值。
	callerSkip int
}

func (o *Options) LoggerConfig() *glog.LogConfig { return o.loggerConfig }
func (o *Options) CallerSkip() int               { return o.callerSkip }

// ApplyOptions 把一组 Option 应用到默认 Options 上，供各引擎构造时复用。
func ApplyOptions(opts ...Option) *Options {
	o := &Options{}
	for _, opt := range opts {
		if opt != nil {
			opt.apply(o)
		}
	}
	return o
}

type optionFunc func(*Options)

func (f optionFunc) apply(o *Options) { f(o) }

// WithLogConfig 指定独立的日志配置；不传时跟随全局 glog（对齐 dbredis/dbgorm）。
func WithLogConfig(logConfig *glog.LogConfig) Option {
	return optionFunc(func(o *Options) { o.loggerConfig = logConfig })
}

// WithCallerSkip 微调日志 caller 校准值；业务在 mq 调用前又套了封装层时使用。
func WithCallerSkip(skip int) Option {
	return optionFunc(func(o *Options) { o.callerSkip = skip })
}
