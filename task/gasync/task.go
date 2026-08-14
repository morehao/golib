package gasync

import (
	"context"
)

type Task interface {
	TypeName() string
	Payload() ([]byte, error)
}

type Handler func(ctx context.Context, payload []byte) error
