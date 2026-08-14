package gasync

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

type emailTask struct {
	To string `json:"to"`
}

func (e emailTask) TypeName() string { return "email:send" }
func (e emailTask) Payload() ([]byte, error) {
	return json.Marshal(e)
}

func TestTaskPayload(t *testing.T) {
	p, err := emailTask{To: "a@b.c"}.Payload()
	require.NoError(t, err)
	require.JSONEq(t, `{"to":"a@b.c"}`, string(p))
}
