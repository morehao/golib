package core

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeObjectKey(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr error
	}{
		{name: "trim and slash normalize", input: "  images\\2026\\a.png  ", want: "images/2026/a.png"},
		{name: "collapse repeated slash", input: "images//2026///a.png", want: "images/2026/a.png"},
		{name: "reject empty", input: "   ", wantErr: ErrInvalidConfig},
		{name: "reject leading slash", input: "/images/a.png", wantErr: ErrInvalidConfig},
		{name: "reject uri", input: "s3://bucket/a.png", wantErr: ErrInvalidConfig},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeObjectKey(tt.input)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}
