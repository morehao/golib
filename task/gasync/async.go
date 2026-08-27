package gasync

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/hibiken/asynq"
	"github.com/morehao/golib/gconstant"
	"github.com/morehao/golib/glog"
	"github.com/morehao/golib/gutil"
	"github.com/morehao/golib/gtrace"
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

	// statusCache 任务启停状态缓存（nil 表示未启用缓存，见 Config.StatusCacheTTL）。
	statusCache *statusCache

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

	c := asynq.NewClient(cfg.redisConnOpt())
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

	var cache *statusCache
	if cfg.StatusCacheTTL > 0 {
		cache = newStatusCache(cfg.StatusCacheTTL)
	}

	s := &Server{
		mux:         mux,
		logger:      logger,
		store:       newStore(getDB),
		cfg:         cfg,
		statusCache: cache,
		taskTypes:   make(map[string]struct{}),
	}

	// 中间件顺序：trace（恢复跨进程上下文）→ 启停开关（丢弃被 Disable 的类型）→ 日志 → 执行记录
	mux.Use(s.traceMiddleware)
	mux.Use(s.disableCheckMiddleware)
	mux.Use(s.logMiddleware)
	mux.Use(s.runRecordMiddleware)

	s.server = asynq.NewServer(cfg.redisConnOpt(), cfg.asynqServerConfig(newAsynqLogger(logger)))

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
	gtrace.T().Inject(ctx, headerCarrier(headers))
	// 透传生产端 request id，消费端恢复后写入 ctx / 执行记录，便于跨进程关联
	if reqID := gutil.GetRequestID(ctx); reqID != "" {
		headers[gconstant.HeaderRequestID] = reqID
	}

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
// 注册时会自动维护任务定义表（core_async_task）：新类型以 enabled 创建，已存在
// （含被 Disable 或软删除的历史行）保留既有状态，重启重新注册不会覆盖运营侧的下线操作。
// 被 Disable 的类型仍会注册 handler，由 disableCheckMiddleware 在运行时丢弃其任务，
// 便于 Enable 后无需重启即恢复消费；注册时输出告警日志提示。
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

	// 自动维护定义表（不覆盖既有启停状态）；定义 upsert 失败不阻断注册，
	// 仅当 DB 不可用时告警，运行期由 disableCheckMiddleware 的 fail-open 兜底。
	if uerr := s.store.upsertTaskOnRegister(context.Background(), &AsyncTask{ID: taskType, Name: taskType}); uerr != nil {
		s.logger.Warnw(context.Background(), "upsert async task definition failed", "task_type", taskType, "error", uerr)
	}
	if !s.store.IsTaskEnabled(context.Background(), taskType) {
		s.logger.Warnw(context.Background(), "async task type is disabled, tasks of this type will be dropped at runtime", "task_type", taskType)
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

// Disable 下线任务类型：DB 定义标记为 disabled，本地缓存同步更新，已投递未消费的任务
// 会被消费端丢弃（见 disableCheckMiddleware），定义保留、可再次 Enable。
// 定义不存在（未注册过或已删除）返回 ErrTaskNotFound；已是 disabled 时幂等返回 nil。
func (s *Server) Disable(taskType string) error {
	if taskType == "" {
		return ErrEmptyTypeName
	}
	if err := s.store.updateTaskStatus(context.Background(), taskType, AsyncTaskDisabled); err != nil {
		return err
	}
	s.refreshStatusCache(taskType, false)
	return nil
}

// Enable 恢复被 Disable 下线的任务类型：DB 定义标记为 enabled，本地缓存同步更新，
// 新投递的任务立即恢复消费，无需重启。定义不存在返回 ErrTaskNotFound；
// 已是 enabled 时幂等返回 nil。
func (s *Server) Enable(taskType string) error {
	if taskType == "" {
		return ErrEmptyTypeName
	}
	if err := s.store.updateTaskStatus(context.Background(), taskType, AsyncTaskEnabled); err != nil {
		return err
	}
	s.refreshStatusCache(taskType, true)
	return nil
}

// isTaskEnabled 判断任务类型是否启用：优先走内存缓存，未命中（或未启用缓存）时查库并回填。
// 查询失败或定义不存在时视为启用（fail-open，与 store.IsTaskEnabled 一致）。
func (s *Server) isTaskEnabled(ctx context.Context, taskType string) bool {
	if s.statusCache == nil {
		return s.store.IsTaskEnabled(ctx, taskType)
	}
	now := time.Now()
	if enabled, ok := s.statusCache.get(taskType, now); ok {
		return enabled
	}
	enabled := s.store.IsTaskEnabled(ctx, taskType)
	s.statusCache.set(taskType, enabled, now)
	return enabled
}

// refreshStatusCache 同步更新本地启停缓存（未启用缓存时为 no-op）。
func (s *Server) refreshStatusCache(taskType string, enabled bool) {
	if s.statusCache == nil {
		return
	}
	s.statusCache.set(taskType, enabled, time.Now())
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
