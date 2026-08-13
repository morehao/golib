package gcron

import (
	"context"
	"fmt"
)

func safeRun(ctx context.Context, fn TaskFunc) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("gcron task panic: %v", r)
		}
	}()
	return fn(ctx)
}
