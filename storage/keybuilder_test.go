package storage

import (
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestKeyBuilderBuild(t *testing.T) {
	now := time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC)
	key := NewKeyBuilder().
		WithNow(func() time.Time { return now }).
		WithPrefix("images").
		WithDateLayout("2006/01/02").
		WithRandomSuffix().
		PreserveExt().
		Build("avatar.png")

	require.Regexp(t, regexp.MustCompile(`^images/2026/05/21/avatar_[a-z0-9]{8}\.png$`), key)
}

func TestKeyBuilderSanitizeName(t *testing.T) {
	key := NewKeyBuilder().WithPrefix("docs").Build("../../Quarter Report.pdf")
	require.Equal(t, "docs/quarter-report.pdf", key)
}
