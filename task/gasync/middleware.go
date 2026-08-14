package gasync

import (
	"context"
	"time"

	"github.com/hibiken/asynq"
	"github.com/morehao/golib/gconstant"
	"github.com/morehao/golib/gtrace"
	"github.com/morehao/golib/gutil"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

const gasyncTracerName = "github.com/morehao/golib/task/gasync"

func (s *Server) logMiddleware(next asynq.Handler) asynq.Handler {
	return asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
		requestID := gutil.GenUUID()
		queue, _ := asynq.GetQueueName(ctx)

		ctx = context.WithValue(ctx, gconstant.KeyAppRequestID, requestID)
		ctx = context.WithValue(ctx, gconstant.KeyRunCode, taskResultID(ctx))
		ctx = context.WithValue(ctx, gconstant.KeyTaskType, "async")

		logger := s.logger.With(
			gconstant.KeyTaskType, "async",
			"queue", queue,
			gconstant.KeyAppRequestID, requestID,
			gconstant.KeyRunCode, taskResultID(ctx),
		)
		start := time.Now()
		logger.Infow(ctx, "async task start", "run_code", taskResultID(ctx))

		err := next.ProcessTask(ctx, task)

		logger.Infow(ctx, "async task done", "run_code", taskResultID(ctx), "duration_ms", time.Since(start).Milliseconds(), "error", err)
		return err
	})
}

func (s *Server) traceMiddleware(next asynq.Handler) asynq.Handler {
	return asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
		carrier := headerCarrier(task.Headers())
		ctx = otel.GetTextMapPropagator().Extract(ctx, carrier)

		tracer := otel.Tracer(gasyncTracerName)
		ctx, span := tracer.Start(ctx, task.Type(), trace.WithSpanKind(trace.SpanKindConsumer))
		defer span.End()

		ctx = gtrace.InjectTraceFields(ctx)

		return next.ProcessTask(ctx, task)
	})
}

func (s *Server) runRecordMiddleware(next asynq.Handler) asynq.Handler {
	return asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
		runCode, _ := asynq.GetTaskID(ctx)
		queue, _ := asynq.GetQueueName(ctx)
		retried, _ := asynq.GetRetryCount(ctx)
		maxRetry, _ := asynq.GetMaxRetry(ctx)

		traceID, _ := ctx.Value(gconstant.KeyTraceID).(string)
		requestID := gutil.GetRequestID(ctx)

		start := time.Now()
		run := &AsyncTaskRun{
			RunCode:   runCode,
			TaskType:  task.Type(),
			Queue:     queue,
			Status:    AsyncProcessing,
			Retried:   retried,
			MaxRetry:  maxRetry,
			StartAt:   &start,
			Payload:   string(task.Payload()),
			TraceID:   traceID,
			RequestID: requestID,
		}

		// asynq 重试复用同一任务 ID（run_code），因此同一任务只保留一行：
		// 首次执行插入，后续重试尝试覆盖该行，最终状态反映最后一次尝试。
		if existing, qerr := s.store.GetRunByRunCode(ctx, runCode); qerr != nil {
			s.logger.Errorw(ctx, "query async run failed", "run_code", runCode, "error", qerr)
		} else if existing.ID != 0 {
			run.ID = existing.ID
			if uerr := s.store.updateRunStart(ctx, run); uerr != nil {
				s.logger.Errorw(ctx, "update async run start failed", "run_code", runCode, "error", uerr)
			}
		} else if serr := s.store.insertRun(ctx, run); serr != nil {
			s.logger.Errorw(ctx, "insert async run failed", "run_code", runCode, "error", serr)
		}

		err := next.ProcessTask(ctx, task)

		end := time.Now()
		status := AsyncCompleted
		errMsg := ""
		if err != nil {
			status = AsyncFailed
			errMsg = err.Error()
		}
		if run.ID != 0 {
			// asynq 超时会取消 ctx，收尾落库需用未取消的 ctx，否则写库静默失败
			if ferr := s.store.finishRun(context.WithoutCancel(ctx), run.ID, end, end.Sub(start).Milliseconds(), status, errMsg); ferr != nil {
				s.logger.Errorw(ctx, "finish async run failed", "run_code", runCode, "error", ferr)
			}
		}
		return err
	})
}

func taskResultID(ctx context.Context) string {
	id, _ := asynq.GetTaskID(ctx)
	return id
}
