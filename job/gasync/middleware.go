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

const gasyncTracerName = "github.com/morehao/golib/job/gasync"

func (s *Server) logMiddleware(next asynq.Handler) asynq.Handler {
	return asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
		requestID := glog.GenRequestID()
		queue, _ := asynq.GetQueueName(ctx)

		ctx = context.WithValue(ctx, glog.KeyAppRequestID, requestID)

		logger := glog.GetDefaultLogger().With(
			"job.type", "async",
			"job.name", task.Type(),
			"queue", queue,
			glog.KeyAppRequestID, requestID,
		)
		start := time.Now()
		logger.Infow(ctx, "async task start", "task_id", taskResultID(ctx))

		err := next.ProcessTask(ctx, task)

		logger.Infow(ctx, "async task done", "task_id", taskResultID(ctx), "duration_ms", time.Since(start).Milliseconds(), "error", err)
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

func taskResultID(ctx context.Context) string {
	id, _ := asynq.GetTaskID(ctx)
	return id
}
