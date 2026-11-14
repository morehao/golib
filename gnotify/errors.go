package gnotify

import "errors"

// gnotify 的哨兵错误。统一带 "gnotify:" 前缀，便于日志检索与错误归类。
//
// 渠道驱动必须让这些错误可被 errors.Is 命中：包装时用
// fmt.Errorf("...: %w", ErrSendFailed) 而不是新造同义错误。
var (
	// ErrInvalidConfig 配置非法：缺必填字段（如 email 的 host/from、wecom 的 webhook）。
	ErrInvalidConfig = errors.New("gnotify: invalid config")

	// ErrChannelNotRegistered 渠道未注册。最常见原因是漏了对应渠道子包的 blank import，
	// 导致其 init() 未执行。见 [New]。
	ErrChannelNotRegistered = errors.New("gnotify: channel not registered")

	// ErrNoRecipient 消息缺少收件人（[Message.To] 为空）。渠道在发送前应返回它，
	// 而不是把空收件人静默当成成功。
	ErrNoRecipient = errors.New("gnotify: no recipient")

	// ErrSendFailed 发送失败。渠道把底层错误包装在它之上
	// （fmt.Errorf("%w: ...", ErrSendFailed)），因此 errors.Is 仍可命中。
	ErrSendFailed = errors.New("gnotify: send failed")
)
