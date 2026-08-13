package gasync

import (
	"context"
	"time"

	"github.com/hibiken/asynq"
	"github.com/morehao/golib/glog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

const gasyncTracerName = "github.com/morehao/golib/task/gasync"

func (s *Server) logMiddleware(next asynq.Handler) asynq.Handler {
	return asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
		requestID := glog.GenRequestID()
		queue, _ := asynq.GetQueueName(ctx)

		ctx = context.WithValue(ctx, glog.KeyAppRequestID, requestID)
		ctx = context.WithValue(ctx, glog.KeyRunCode, taskResultID(ctx))
		ctx = context.WithValue(ctx, glog.KeyTaskType, "async")

		logger := s.logger.With(
			glog.KeyTaskType, "async",
			"queue", queue,
			glog.KeyAppRequestID, requestID,
			glog.KeyRunCode, taskResultID(ctx),
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
		prop := propagation.TraceContext{}
		carrier := headerCarrier(task.Headers())
		ctx = prop.Extract(ctx, carrier)

		tracer := otel.Tracer(gasyncTracerName)
		ctx, span := tracer.Start(ctx, task.Type(), trace.WithSpanKind(trace.SpanKindConsumer))
		defer span.End()

		spanCtx := span.SpanContext()
		ctx = context.WithValue(ctx, glog.KeyTraceID, spanCtx.TraceID().String())
		ctx = context.WithValue(ctx, glog.KeySpanID, spanCtx.SpanID().String())

		return next.ProcessTask(ctx, task)
	})
}

func (s *Server) executionRecordMiddleware(next asynq.Handler) asynq.Handler {
	return asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
		runCode, _ := asynq.GetTaskID(ctx)
		queue, _ := asynq.GetQueueName(ctx)
		retried, _ := asynq.GetRetryCount(ctx)
		maxRetry, _ := asynq.GetMaxRetry(ctx)

		traceID, _ := ctx.Value(glog.KeyTraceID).(string)
		requestID := glog.GetRequestID(ctx)

		start := time.Now()
		exec := &AsyncTaskRun{
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
		if serr := s.store.insertExecution(ctx, exec); serr != nil {
			s.logger.Errorw(ctx, "insert async execution failed", "run_code", runCode, "error", serr)
		}

		err := next.ProcessTask(ctx, task)

		end := time.Now()
		status := AsyncCompleted
		errMsg := ""
		if err != nil {
			status = AsyncFailed
			errMsg = err.Error()
		}
		if exec.ID != 0 {
			if ferr := s.store.finishExecution(ctx, exec.ID, end, end.Sub(start).Milliseconds(), status, errMsg); ferr != nil {
				s.logger.Errorw(ctx, "finish async execution failed", "run_code", runCode, "error", ferr)
			}
		}
		return err
	})
}

func taskResultID(ctx context.Context) string {
	id, _ := asynq.GetTaskID(ctx)
	return id
}
