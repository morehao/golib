package jwtauth

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIssueOptionsApplied(t *testing.T) {
	type CustomData struct {
		Role string `json:"role"`
	}

	now := time.Now()
	auth, err := New[CustomData]("secret")
	require.NoError(t, err)

	token, err := auth.Issue(
		"user123",
		"example.com",
		time.Now().Add(2*time.Hour),
		CustomData{Role: "admin"},
		WithAudience[CustomData]("audience1", "audience2"),
		WithNotBefore[CustomData](now),
		WithID[CustomData]("unique-id-12345"),
	)
	require.NoError(t, err)

	claims, err := auth.Parse(token)
	require.NoError(t, err)
	assert.Equal(t, "admin", claims.CustomData.Role)
	assert.Equal(t, "example.com", claims.Issuer)
	assert.Equal(t, "unique-id-12345", claims.ID)
	assert.Equal(t, 2, len(claims.Audience))
	assert.NotNil(t, claims.NotBefore)
}
