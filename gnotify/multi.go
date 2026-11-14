package gnotify

import (
	"context"
	"errors"

	"golang.org/x/sync/errgroup"
)

// Strategy 定义 [MultiSender] 向多个渠道投递时的策略。
type Strategy int

const (
	// BroadcastAll 尽力而为广播：并发发给所有渠道，收集全部错误并用 errors.Join 聚合。
	// 部分渠道失败时返回的 error 非 nil（包含失败明细），但成功的渠道已经送达；
	// 需要精细处理时可改用 [MultiSender.SendResult] 查看逐渠道结果。
	BroadcastAll Strategy = iota

	// RequireAll 全员必达：并发发给所有渠道，语义上要求全部成功。
	// 与 BroadcastAll 的实现差异仅在于语义表达——错误同样聚合返回，
	// 调用方按「任一失败即视为整体失败」处理即可。
	RequireAll

	// FirstSuccess 降级：按顺序串行尝试，第一个成功的渠道即返回 nil；
	// 全部失败时返回聚合错误。适用于「主渠道失败才用备用渠道」的场景。
	FirstSuccess
)

// ChannelResult 是 [MultiSender] 对单渠道的投递结果。
type ChannelResult struct {
	Channel string
	Err     error // nil 表示该渠道发送成功
}

// Result 是 [MultiSender.SendResult] 的结构化结果，便于调用方按渠道处置
// （例如只重发失败的渠道）。
type Result struct {
	Results []ChannelResult
}

// OK 报告是否所有渠道都成功（无渠道时视为成功）。
func (r Result) OK() bool {
	for _, cr := range r.Results {
		if cr.Err != nil {
			return false
		}
	}
	return true
}

// Failed 返回失败的渠道结果（Err != nil）。
func (r Result) Failed() []ChannelResult {
	var out []ChannelResult
	for _, cr := range r.Results {
		if cr.Err != nil {
			out = append(out, cr)
		}
	}
	return out
}

// MultiSender 把多个已构造的 Notifier 组合，一次向它们投递同一条消息。
//
// 与 [Router] 的分工：
//   - Router 按渠道名精确发「单个」渠道，不做隐式广播；
//   - MultiSender 表达「一次事件同时通知多个平台」的显式多播语义，
//     通过 Strategy 区分广播/必达/降级，返回逐渠道结果。
type MultiSender struct {
	notifiers []Notifier
	strategy  Strategy
	// maxConcurrency 是并发策略下的最大并发投递数；<=0 时取 DefaultMultiConcurrency。
	maxConcurrency int
}

// DefaultMultiConcurrency 是 [MultiSender] 并发投递的默认最大并发数。
const DefaultMultiConcurrency = 8

// MultiOption 修改 [MultiSender] 的行为。
type MultiOption func(*MultiSender)

// WithMaxConcurrency 设置并发投递的最大并发数（仅对非 FirstSuccess 策略有效）。
// n <= 0 表示使用 [DefaultMultiConcurrency]。
func WithMaxConcurrency(n int) MultiOption {
	return func(m *MultiSender) { m.maxConcurrency = n }
}

// NewMultiSender 用策略与渠道构造 MultiSender。
//
// 传入的 Notifier 按 [Notifier.Channel] 去重（同名后者覆盖前者），nil 与空名项被跳过。
func NewMultiSender(s Strategy, notifiers ...Notifier) *MultiSender {
	m := &MultiSender{strategy: s, maxConcurrency: DefaultMultiConcurrency}
	seen := make(map[string]int, len(notifiers))
	for _, n := range notifiers {
		if n == nil {
			continue
		}
		name := n.Channel()
		if name == "" {
			continue
		}
		if idx, ok := seen[name]; ok {
			m.notifiers[idx] = n // 后者覆盖前者
			continue
		}
		seen[name] = len(m.notifiers)
		m.notifiers = append(m.notifiers, n)
	}
	return m
}

// channels 返回已去重的渠道名列表（仅用于诊断/日志）。
func (m *MultiSender) channels() []string {
	out := make([]string, 0, len(m.notifiers))
	for _, n := range m.notifiers {
		out = append(out, n.Channel())
	}
	return out
}

// Send 向所有渠道投递 msg，把逐渠道结果聚合成一个 error（全部成功返回 nil）。
//
// 需要逐渠道明细时请用 [MultiSender.SendResult]。
func (m *MultiSender) Send(ctx context.Context, msg Message) error {
	_, err := m.SendResult(ctx, msg)
	return err
}

// SendResult 向所有渠道投递 msg，返回逐渠道结果与聚合 error。
//
// 聚合 error 为各失败渠道错误之和（errors.Join），全部成功时为 nil。
// 注意：即便返回 error，成功的渠道也已送达——Result 中可区分。
func (m *MultiSender) SendResult(ctx context.Context, msg Message) (Result, error) {
	if m == nil || len(m.notifiers) == 0 {
		return Result{}, nil
	}

	if m.strategy == FirstSuccess {
		return m.sendFirstSuccess(ctx, msg)
	}
	return m.sendConcurrent(ctx, msg)
}

// sendFirstSuccess 串行尝试，首个成功即返回 nil；全失败返回聚合错误。
func (m *MultiSender) sendFirstSuccess(ctx context.Context, msg Message) (Result, error) {
	var results []ChannelResult
	var errs []error
	for _, n := range m.notifiers {
		err := n.Send(ctx, msg)
		results = append(results, ChannelResult{Channel: n.Channel(), Err: err})
		if err == nil {
			// 首个成功即止，后续渠道不再尝试（降级语义）。
			return Result{Results: results}, nil
		}
		errs = append(errs, err)
	}
	return Result{Results: results}, errors.Join(errs...)
}

// sendConcurrent 并发投递，收集全部结果并聚合错误。
func (m *MultiSender) sendConcurrent(ctx context.Context, msg Message) (Result, error) {
	n := len(m.notifiers)
	results := make([]ChannelResult, n)

	limit := m.maxConcurrency
	if limit <= 0 {
		limit = DefaultMultiConcurrency
	}

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(limit)
	for i, notifier := range m.notifiers {
		i, notifier := i, notifier
		g.Go(func() error {
			err := notifier.Send(gctx, msg)
			results[i] = ChannelResult{Channel: notifier.Channel(), Err: err}
			// 不在此处 return err：保留「单个渠道失败不影响其它渠道」的语义，
			// 由下面统一聚合。
			return nil
		})
	}
	_ = g.Wait()

	var errs []error
	for _, cr := range results {
		if cr.Err != nil {
			errs = append(errs, cr.Err)
		}
	}
	return Result{Results: results}, errors.Join(errs...)
}
