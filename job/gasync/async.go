package gasync

import (
	"context"

	"github.com/hibiken/asynq"
	"github.com/morehao/golib/glog"
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
		return nil, errEmptyAddr
	}

	c := asynq.NewClient(cfg.asynqRedisOpt())
	logger, _ := newGasyncLogger(cfg)

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
		return nil, errEmptyAddr
	}

	mux := asynq.NewServeMux()
	logger, _ := newGasyncLogger(cfg)

	getDB := func(ctx context.Context) *gorm.DB { return db.WithContext(ctx) }

	s := &Server{
		mux:    mux,
		logger: logger,
		store:  newStore(getDB),
		cfg:    cfg,
	}

	mux.Use(s.traceMiddleware)
	mux.Use(s.logMiddleware)

	s.server = asynq.NewServer(cfg.asynqRedisOpt(), cfg.asynqServerConfig())

	return s, nil
}

func (c *Client) Enqueue(ctx context.Context, t Task, opts ...asynq.Option) (*asynq.TaskInfo, error) {
	if t.TypeName() == "" {
		return nil, errEmptyTypeName
	}
	payload, err := t.Payload()
	if err != nil {
		return nil, err
	}

	fullOpts := []asynq.Option{
		asynq.MaxRetry(c.cfg.MaxRetry),
		asynq.Timeout(c.cfg.Timeout),
		asynq.Retention(c.cfg.Retention),
	}
	fullOpts = append(fullOpts, opts...)

	task := asynq.NewTask(t.TypeName(), payload, fullOpts...)
	return c.client.EnqueueContext(ctx, task)
}

func (s *Server) Register(taskType string, h Handler) error {
	if taskType == "" {
		return errEmptyTypeName
	}
	if h == nil {
		return errNilHandler
	}
	s.mux.HandleFunc(taskType, func(ctx context.Context, task *asynq.Task) error {
		return h(ctx, task.Payload())
	})
	return nil
}

func (s *Server) Run() error {
	return s.server.Run(s.mux)
}

func (s *Server) Shutdown() {
	s.server.Shutdown()
}

func (s *Server) GetStore() *store {
	return s.store
}
