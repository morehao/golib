package gcontext

import "context"

func NilCtx(ctx context.Context) bool {
	return ctx == nil
}
