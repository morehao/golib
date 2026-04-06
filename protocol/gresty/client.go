package gresty

import (
	"github.com/morehao/golib/glog"
	"github.com/morehao/golib/protocol"
	"resty.dev/v3"
)

type Client struct {
	*resty.Client
	logger glog.Logger
}

func NewClient() *Client {
	logCfg := glog.GetLoggerConfig()
	logger, err := glog.NewLogger(logCfg)
	if err != nil {
		logger = glog.GetDefaultLogger()
	}

	c := &Client{
		Client: resty.New(),
		logger: logger,
	}

	c.SetLogger(newGlogAdapter(logger))
	c.SetDebug(false)
	c.AddRequestMiddleware(func(client *resty.Client, req *resty.Request) error {
		req.Header = protocol.InjectTraceAndRequestID(req.Context(), req.Header)
		return nil
	})

	c.AddResponseMiddleware(func(client *resty.Client, resp *resty.Response) error {
		return newLoggingMiddleware(logger).handle(resp)
	})

	return c
}
