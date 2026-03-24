package gresty

import (
	"context"

	"github.com/morehao/golib/glog"
)

type glogAdapter struct {
	logger glog.Logger
}

func newGlogAdapter(logger glog.Logger) *glogAdapter {
	return &glogAdapter{logger: logger}
}

func (g *glogAdapter) Debugf(format string, v ...any) {
	g.logger.Debugf(context.Background(), format, v...)
}

func (g *glogAdapter) Errorf(format string, v ...any) {
	g.logger.Errorf(context.Background(), format, v...)
}

func (g *glogAdapter) Warnf(format string, v ...any) {
	g.logger.Warnf(context.Background(), format, v...)
}
