package gasync

import (
	"context"
	"fmt"
	"sync"

	"github.com/hibiken/asynq"
	"github.com/morehao/golib/glog"
	"go.opentelemetry.io/otel"
	"gorm.io/gorm"
)

type Client struct {
	client *asynq.Client
	logger glog.Logger
	cfg    *Config
}

type Server struct {
	server *asynq.Server
	mux    *asynq.ServeMux
	logger glog.Logger
	store  *store
	cfg    *Config

	mu        sync.Mutex
	taskTypes map[string]struct{}
}

func NewClient(cfg *Config, opts ...Option) (*Client, error) {
	if cfg == nil {
		cfg = defaultConfig()
	}
	for _, opt := range opts {
		if opt != nil {
			opt.apply(cfg)
		}
	}
	if cfg.RedisAddr == "" {
		return nil, ErrEmptyAddr
	}

	c := asynq.NewClient(cfg.asynqRedisOpt())
	logger := newGasyncLogger()

	return &Client{client: c, logger: logger, cfg: cfg}, nil
}

func NewServer(cfg *Config, db *gorm.DB, opts ...Option) (*Server, error) {
	if cfg == nil {
		cfg = defaultConfig()
	}
	for _, opt := range opts {
		if opt != nil {
			opt.apply(cfg)
		}
	}
	if cfg.RedisAddr == "" {
		return nil, ErrEmptyAddr
	}
	if db == nil {
		return nil, ErrNilDB
	}

	mux := asynq.NewServeMux()
	logger := newGasyncLogger()

	getDB := func(ctx context.Context) *gorm.DB { return db.WithContext(ctx) }

	s := &Server{
		mux:       mux,
		logger:    logger,
		store:     newStore(getDB),
		cfg:       cfg,
		taskTypes: make(map[string]struct{}),
	}

	mux.Use(s.traceMiddleware)
	mux.Use(s.logMiddleware)
	mux.Use(s.runRecordMiddleware)

	s.server = asynq.NewServer(cfg.asynqRedisOpt(), cfg.asynqServerConfig())

	return s, nil
}

func (c *Client) Enqueue(ctx context.Context, t Task, opts ...asynq.Option) (*asynq.TaskInfo, error) {
	if t.TypeName() == "" {
		return nil, ErrEmptyTypeName
	}
	payload, err := t.Payload()
	if err != nil {
		return nil, fmt.Errorf("gasync: marshal task payload: %w", err)
	}

	fullOpts := []asynq.Option{
		asynq.MaxRetry(c.cfg.MaxRetry),
		asynq.Timeout(c.cfg.Timeout),
		asynq.Retention(c.cfg.Retention),
	}
	fullOpts = append(fullOpts, opts...)

	headers := make(map[string]string)
	otel.GetTextMapPropagator().Inject(ctx, headerCarrier(headers))

	task := asynq.NewTaskWithHeaders(t.TypeName(), payload, headers, fullOpts...)
	return c.client.EnqueueContext(ctx, task)
}

// Close 释放生产端与 Redis 的连接。
func (c *Client) Close() error {
	if c.client == nil {
		return nil
	}
	return c.client.Close()
}

// Register 注册任务处理器。同一任务类型在进程内重复注册返回 ErrDuplicateTaskType。
func (s *Server) Register(taskType string, h Handler) error {
	if taskType == "" {
		return ErrEmptyTypeName
	}
	if h == nil {
		return ErrNilHandler
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.taskTypes[taskType]; ok {
		return ErrDuplicateTaskType
	}

	s.mux.HandleFunc(taskType, func(ctx context.Context, task *asynq.Task) error {
		return h(ctx, task.Payload())
	})
	s.taskTypes[taskType] = struct{}{}
	return nil
}

// Use 追加自定义中间件（必须在 Run 之前调用）。
func (s *Server) Use(mws ...asynq.MiddlewareFunc) {
	s.mux.Use(mws...)
}

func (s *Server) Run() error {
	return s.server.Run(s.mux)
}

// Shutdown 优雅停机：等待在途任务完成，时长受 cfg.ShutdownTimeout 约束。
func (s *Server) Shutdown() {
	s.server.Shutdown()
}

// ShutdownContext 带上下文约束的优雅停机：ctx 先于停机完成时返回 ctx.Err()。
// 注意：底层 asynq 服务器仍在后台继续停机（至多 ShutdownTimeout），不会泄漏无限期。
func (s *Server) ShutdownContext(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		s.server.Shutdown()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Server) GetStore() *store {
	return s.store
}
