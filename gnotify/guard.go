package gnotify

import "context"

// guardedNotifier 是 [Config.Disabled] / [Config.DryRun] 的统一落点：
// 通过装饰所有渠道，使开关对「任何构造方式」（注册表 New、各渠道 New/NewWithCore）一致生效，
// 而各渠道自己的 Send 无需感知这两个开关。
type guardedNotifier struct {
	inner   Notifier
	channel string
	cfg     Config
}

// Channel 实现 [Notifier]。
func (g *guardedNotifier) Channel() string { return g.channel }

// Send 实现 [Notifier]：
//   - Disabled：直接返回 nil，不发送、不打日志（整体静音）；
//   - DryRun：不发送，但打一条 dry-run 日志便于预览；
//   - 否则透传给被包装的渠道。
func (g *guardedNotifier) Send(ctx context.Context, msg Message) error {
	if g.cfg.Disabled {
		return nil
	}
	if g.cfg.DryRun {
		g.cfg.logger().Infow(ctx, "gnotify: dry-run drop",
			"channel", g.channel,
			"to", msg.To,
			"subject", msg.Subject,
			"body_len", len(msg.Body),
		)
		return nil
	}
	return g.inner.Send(ctx, msg)
}

// Guard 用 cfg 的 Disabled/DryRun 包裹渠道 n，供渠道子包在构造完成后统一应用开关。
//
// 当两个开关都未开启时原样返回 n（零额外开销）；n 为 nil 时返回 nil。
func Guard(n Notifier, cfg Config) Notifier {
	return guard(n, cfg)
}

// guard 用 Config 中的 Disabled/DryRun 包裹一个渠道。
//
// 当两个开关都未开启时原样返回 inner（零额外开销），否则返回装饰器。
// inner 为 nil 时原样返回 nil（交由上层判定，避免把 nil 包成非 nil）。
func guard(n Notifier, cfg Config) Notifier {
	if n == nil {
		return nil
	}
	if !cfg.Disabled && !cfg.DryRun {
		return n
	}
	return &guardedNotifier{inner: n, channel: n.Channel(), cfg: cfg}
}
