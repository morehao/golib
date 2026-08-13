package gasync

import (
	"testing"

	"github.com/stretchr/testify/require"
)

type emailTask struct {
	To string `json:"to"`
}

func (e emailTask) TypeName() string { return "email:send" }
func (e emailTask) Payload() ([]byte, error) {
	return jsonPayload(e)
}

func TestJSONPayload(t *testing.T) {
	p, err := jsonPayload(emailTask{To: "a@b.c"})
	require.NoError(t, err)
	require.JSONEq(t, `{"to":"a@b.c"}`, string(p))
}
