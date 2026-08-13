package gasync

import (
	"context"
	"encoding/json"
)

type Task interface {
	TypeName() string
	Payload() ([]byte, error)
}

type Handler func(ctx context.Context, payload []byte) error

func jsonPayload(v any) ([]byte, error) {
	return json.Marshal(v)
}
