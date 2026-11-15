package worker

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"strings"
	"time"

	"github.com/morehao/golib/task/gasync"
)

// Handler 是唯一必须实现的方法。
type Handler interface {
	Execute(ctx context.Context, task *gasync.AsyncTask) error
}

// HandlerFunc 便于「只写一个函数」的场景。
func HandlerFunc(fn func(ctx context.Context, t *gasync.AsyncTask) error) Handler {
	return handlerFunc(fn)
}

type handlerFunc func(ctx context.Context, t *gasync.AsyncTask) error

func (f handlerFunc) Execute(ctx context.Context, t *gasync.AsyncTask) error { return f(ctx, t) }

// Lifecycle 是可选钩子，实现几个算几个。
type Lifecycle interface {
	BeforeExecute(ctx context.Context, task *gasync.AsyncTask) error
	OnRetry(ctx context.Context, task *gasync.AsyncTask, cause error) error
	OnSuccess(ctx context.Context, task *gasync.AsyncTask) error
	OnFailure(ctx context.Context, task *gasync.AsyncTask, cause error) error
	OnCanceled(ctx context.Context, task *gasync.AsyncTask) error
}

// 控制流哨兵。
var (
	// ErrSkip 不执行 Execute，任务直接 skipped（BeforeExecute 返回）。
	ErrSkip = errors.New("gasync: skip this task")
	// ErrNoRetry 立即 failed，不消耗剩余次数（Execute 返回）。
	ErrNoRetry = errors.New("gasync: do not retry")
)

// buildCallback 按 outcome 构造交回 Source 的 EnsureFunc；把本次失败原因注入闭包。
//
// 说明：「失败是否可重试」最终由 Source 判定（次数是否用尽、ErrNoRetry 等）。worker 只能
// 基于错误语义给出初判：ErrNoRetry / panic 视为不可重试（选 OnFailure），timeout /
// handler_error 视为可重试（选 OnRetry）。Source 在事务内调用该回调完成状态与钩子的同步落库。
func (w *Worker) buildCallback(task *gasync.AsyncTask, outcome gasync.Outcome, kind gasync.FailureKind, cause error) gasync.EnsureFunc {
	if outcome == gasync.OutcomeSkipped {
		return nil
	}
	entry := w.handlers[task.Type]
	if entry == nil {
		return nil
	}
	lc := asLifecycle(entry.handler)
	if lc == nil {
		return nil
	}

	switch outcome {
	case gasync.OutcomeSucceeded:
		return func(ctx context.Context) error { return lc.OnSuccess(ctx, task) }
	case gasync.OutcomeCanceled:
		return func(ctx context.Context) error { return lc.OnCanceled(ctx, task) }
	case gasync.OutcomeFailed:
		// 失败能否重试：panic / ErrNoRetry 一律不可重试；其余按「剩余次数」初判。
		// 最终是否重试仍由 Source 判定，这里只决定 worker 交给 Source 的默认回调。
		if kind == gasync.FailurePanic || errors.Is(cause, ErrNoRetry) || !hasRemainingAttempts(task) {
			return func(ctx context.Context) error { return lc.OnFailure(ctx, task, cause) }
		}
		return func(ctx context.Context) error { return lc.OnRetry(ctx, task, cause) }
	}
	return nil
}

// hasRemainingAttempts 判断任务是否还有剩余重试次数（MaxAttempts<=0 视为不设上限）。
func hasRemainingAttempts(task *gasync.AsyncTask) bool {
	if task.MaxAttempts <= 0 {
		return true
	}
	return task.Attempt < task.MaxAttempts
}

// asLifecycle 把 Handler 收敛成可选钩子：未实现 Lifecycle 时返回 nil。
func asLifecycle(h Handler) Lifecycle {
	if lc, ok := h.(Lifecycle); ok {
		return lc
	}
	return nil
}

// process 处理单条任务：BeforeExecute → Execute（带超时/recover）→ 分类 → Save。
func (w *Worker) process(taskType string, task *gasync.AsyncTask) {
	start := time.Now()
	if task.Type == "" {
		task.Type = taskType
	}
	w.emit("started", task, 0, 0, nil)

	lc := asLifecycle(w.handlers[taskType].handler)

	// BeforeExecute 不在事务内；返回 ErrSkip 时写 skipped，无回调。
	if lc != nil {
		if err := lc.BeforeExecute(w.baseCtx, task); err != nil {
			if errors.Is(err, ErrSkip) {
				w.save(task, gasync.Result{
					Outcome:  gasync.OutcomeSkipped,
					Failure:  gasync.FailureSkipped,
					Duration: time.Since(start),
				}, nil)
				w.emit("skipped", task, 0, time.Since(start), nil)
				return
			}
			// BeforeExecute 的其它错误按 handler_error 失败处理（按 outcome 走失败链路）。
			res := gasync.Result{
				Outcome:  gasync.OutcomeFailed,
				Failure:  gasync.FailureHandler,
				Err:      err,
				Duration: time.Since(start),
			}
			w.save(task, res, w.buildCallback(task, res.Outcome, res.Failure, err))
			w.emit("failed", task, 0, time.Since(start), err)
			return
		}
	}

	timeout := w.execTimeout(task)
	execCtx, cancel := context.WithTimeout(w.baseCtx, timeout)

	payload, err, panicked := safeExecute(execCtx, w.handlers[taskType].handler, task)
	cancel()

	res := gasync.Result{
		Payload:  payload,
		Duration: time.Since(start),
		Err:      err,
	}

	switch {
	case panicked:
		res.Outcome = gasync.OutcomeFailed
		res.Failure = gasync.FailurePanic
	case err == nil:
		res.Outcome = gasync.OutcomeSucceeded
	case errors.Is(err, ErrNoRetry):
		res.Outcome = gasync.OutcomeFailed
		res.Failure = gasync.FailureHandler
	case execCtxErrDeadline(execCtx, err):
		res.Outcome = gasync.OutcomeFailed
		res.Failure = gasync.FailureTimeout
	case w.baseCtx.Err() != nil && execCtx.Err() != context.DeadlineExceeded:
		// 执行期间被停机/取消。
		res.Outcome = gasync.OutcomeCanceled
		res.Failure = gasync.FailureCanceled
	default:
		res.Outcome = gasync.OutcomeFailed
		res.Failure = gasync.FailureHandler
	}

	cb := w.buildCallback(task, res.Outcome, res.Failure, err)
	w.save(task, res, cb)

	switch res.Outcome {
	case gasync.OutcomeSucceeded:
		w.emit("succeeded", task, 0, res.Duration, nil)
	case gasync.OutcomeCanceled:
		w.emit("canceled", task, 0, res.Duration, err)
	default:
		w.emit("failed", task, 0, res.Duration, err)
	}
}

// execCtxErrDeadline 判断是否因 ctx 超时而失败。
func execCtxErrDeadline(execCtx context.Context, err error) bool {
	if err != nil && errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	return execCtx.Err() == context.DeadlineExceeded && err != nil
}

// execTimeout 解析本次执行超时：任务显式 Timeout > HandlerOption > 默认 30s。
func (w *Worker) execTimeout(task *gasync.AsyncTask) time.Duration {
	if task.Timeout > 0 {
		return task.Timeout
	}
	if e := w.handlers[task.Type]; e != nil && e.opts.timeout > 0 {
		return e.opts.timeout
	}
	return 30 * time.Second
}

// safeExecute 调 Execute，用 recover 捕获 panic；返回 (payload, err, panicked)。
func safeExecute(ctx context.Context, h Handler, task *gasync.AsyncTask) (payload []byte, err error, panicked bool) {
	defer func() {
		if r := recover(); r != nil {
			stack := debug.Stack()
			top := topStackFrame(stack)
			err = fmt.Errorf("gasync: handler panic: %v\n%s", r, top)
			panicked = true
		}
	}()
	err = h.Execute(ctx, task)
	return nil, err, false
}

func topStackFrame(stack []byte) string {
	s := string(stack)
	lines := strings.Split(s, "\n")
	var out []string
	for _, ln := range lines {
		t := strings.TrimSpace(ln)
		if t == "" || strings.HasPrefix(t, "goroutine ") || strings.HasPrefix(t, "panic: ") {
			continue
		}
		out = append(out, ln)
		if len(out) >= 3 {
			break
		}
	}
	if len(out) == 0 {
		return s
	}
	return strings.Join(out, "\n")
}

// save 调 src.Save；错误仅记日志（Source 负责重建/兜底，worker 不重试 Save）。
func (w *Worker) save(task *gasync.AsyncTask, res gasync.Result, cb gasync.EnsureFunc) {
	if err := w.src.Save(w.baseCtx, task, res, cb); err != nil {
		w.logger.Errorw(w.baseCtx, "gasync worker save error",
			"task_type", task.Type, "job_id", task.ID, "outcome", res.Outcome, "error", err)
	}
}

func (w *Worker) emit(typ string, task *gasync.AsyncTask, attempt int, d time.Duration, err error) {
	if w.cfg.EventHandler == nil {
		return
	}
	w.cfg.EventHandler(Event{
		Type:     typ,
		AsyncTaskID:    task.ID,
		TaskType: task.Type,
		WorkerID: w.cfg.WorkerID,
		Attempt:  attempt,
		Duration: d,
		Err:      err,
	})
}
