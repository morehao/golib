package core

import (
	"testing"

	"github.com/morehao/golib/storage"
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
		{name: "happy path clean input", input: "images/2026/a.png", want: "images/2026/a.png"},
		{name: "reject empty", input: "   ", wantErr: storage.ErrInvalidKey},
		{name: "reject leading slash", input: "/images/a.png", wantErr: storage.ErrInvalidKey},
		{name: "reject trailing slash", input: "images/a.png/", wantErr: storage.ErrInvalidKey},
		{name: "reject uri", input: "s3://bucket/a.png", wantErr: storage.ErrInvalidKey},
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

func TestValidateObjectKey(t *testing.T) {
	require.NoError(t, ValidateObjectKey("valid/key.txt"))
	require.Error(t, ValidateObjectKey("/invalid"))
	require.Error(t, ValidateObjectKey(""))
}
