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

const tracerName = "github.com/morehao/golib/task/gasync"

// 落库字段长度上限，避免大 payload / 超长错误信息撑大执行记录表。
const (
	maxPayloadLen  = 4096
	maxErrorMsgLen = 1024
)

func (s *Server) logMiddleware(next asynq.Handler) asynq.Handler {
	return asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
		// 优先复用生产端透传的 request id，未透传时生成新的
		requestID := gutil.GetRequestID(ctx)
		if requestID == "" {
			requestID = gutil.GenUUID()
		}
		runID, _ := asynq.GetTaskID(ctx)
		queue, _ := asynq.GetQueueName(ctx)

		ctx = context.WithValue(ctx, gconstant.KeyAppRequestID, requestID)
		ctx = context.WithValue(ctx, gconstant.KeyRunID, runID)
		ctx = context.WithValue(ctx, gconstant.KeyTaskType, "async")

		// 运行 ID（task.run.id）通过 ctx 由 glog extra_keys 打印，不再重复写入 With 字段
		logger := s.logger.With(
			gconstant.KeyTaskType, "async",
			"queue", queue,
			gconstant.KeyAppRequestID, requestID,
		)
		start := time.Now()
		logger.Infow(ctx, "async task start")

		err := next.ProcessTask(ctx, task)

		doneFields := []any{"duration_ms", time.Since(start).Milliseconds()}
		if err != nil {
			doneFields = append(doneFields, "error", err)
		}
		logger.Infow(ctx, "async task done", doneFields...)
		return err
	})
}

func (s *Server) traceMiddleware(next asynq.Handler) asynq.Handler {
	return asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
		carrier := headerCarrier(task.Headers())
		ctx = otel.GetTextMapPropagator().Extract(ctx, carrier)

		// 透传生产端 request id（与 Enqueue 侧注入对应）
		if reqID := carrier.Get(gconstant.HeaderRequestID); reqID != "" {
			ctx = context.WithValue(ctx, gconstant.KeyAppRequestID, reqID)
		}

		tracer := otel.Tracer(tracerName)
		ctx, span := tracer.Start(ctx, task.Type(), trace.WithSpanKind(trace.SpanKindConsumer))
		defer span.End()

		ctx = gtrace.InjectTraceFields(ctx)

		return next.ProcessTask(ctx, task)
	})
}

func (s *Server) runRecordMiddleware(next asynq.Handler) asynq.Handler {
	return asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
		runID, _ := asynq.GetTaskID(ctx)
		queue, _ := asynq.GetQueueName(ctx)
		retried, _ := asynq.GetRetryCount(ctx)
		maxRetry, _ := asynq.GetMaxRetry(ctx)

		requestID := gutil.GetRequestID(ctx)

		start := time.Now()
		run := &AsyncTaskRun{
			ID:        runID,
			TaskType:  task.Type(),
			Queue:     queue,
			Status:    AsyncProcessing,
			Retried:   retried,
			MaxRetry:  maxRetry,
			StartAt:   &start,
			Payload:   gutil.TruncateString(string(task.Payload()), maxPayloadLen),
			RequestID: requestID,
		}

		// asynq 重试复用同一任务 ID（主键 id），同一任务只保留一行：
		// 原子 upsert 覆盖开始信息，兼容 at-least-once 下同一任务被并发处理的场景，
		// 避免"先查再写"竞态导致重复插入撞主键而丢失执行记录。
		if uerr := s.store.upsertRunStart(ctx, run); uerr != nil {
			s.logger.Errorw(ctx, "upsert async run failed", gconstant.KeyRunID, runID, "error", uerr)
		}

		err := next.ProcessTask(ctx, task)

		end := time.Now()
		status := AsyncCompleted
		errMsg := ""
		if err != nil {
			status = AsyncFailed
			errMsg = gutil.TruncateString(err.Error(), maxErrorMsgLen)
		}
		if run.ID != "" {
			// asynq 超时会取消 ctx，收尾落库需用未取消的 ctx，否则写库静默失败
			if ferr := s.store.finishRun(context.WithoutCancel(ctx), run.ID, end, end.Sub(start).Milliseconds(), status, errMsg); ferr != nil {
				s.logger.Errorw(ctx, "finish async run failed", gconstant.KeyRunID, runID, "error", ferr)
			}
		}
		return err
	})
}
