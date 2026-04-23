package jwtauth

import (
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIssueAndParse(t *testing.T) {
	type CustomData struct {
		Role string `json:"role"`
	}

	auth, err := New[CustomData]("secret")
	require.NoError(t, err)

	token, err := auth.Issue(
		"user123",
		"example.com",
		time.Now().Add(24*time.Hour),
		CustomData{Role: "admin"},
		WithID[CustomData]("id-1"),
	)
	require.NoError(t, err)

	parsedClaims, err := auth.Parse(token)
	require.NoError(t, err)

	assert.Equal(t, "admin", parsedClaims.CustomData.Role)
	assert.Equal(t, "user123", parsedClaims.Subject)
	assert.Equal(t, "example.com", parsedClaims.Issuer)
	assert.Empty(t, parsedClaims.Audience)
	assert.Equal(t, "id-1", parsedClaims.ID)
}

func TestIssueValidation(t *testing.T) {
	type CustomData struct {
		Role string `json:"role"`
	}

	_, err := New[CustomData]("")
	assert.True(t, errors.Is(err, ErrEmptySignKey))

	auth, err := New[CustomData]("secret")
	require.NoError(t, err)

	_, err = auth.Issue("", "example.com", time.Now().Add(time.Hour), CustomData{Role: "admin"})
	assert.True(t, errors.Is(err, ErrEmptySubject))

	_, err = auth.Issue("user123", "", time.Now().Add(time.Hour), CustomData{Role: "admin"})
	assert.True(t, errors.Is(err, ErrEmptyIssuer))

	_, err = auth.Issue("user123", "example.com", time.Now().Add(time.Hour), CustomData{Role: "admin"})
	require.NoError(t, err)

	_, err = auth.Issue("user123", "example.com", time.Now().Add(-time.Minute), CustomData{Role: "admin"})
	assert.True(t, errors.Is(err, ErrInvalidExpiry))
}

func TestParseTokenValidation(t *testing.T) {
	type CustomData struct {
		Role string `json:"role"`
	}

	auth, err := New[CustomData]("secret")
	require.NoError(t, err)
	token, err := auth.Issue("user123", "example.com", time.Now().Add(time.Hour), CustomData{Role: "admin"})
	require.NoError(t, err)

	_, err = auth.Parse("")
	assert.True(t, errors.Is(err, ErrEmptyToken))

	wrongAuth, err := New[CustomData]("wrong-secret")
	require.NoError(t, err)
	_, err = wrongAuth.Parse(token)
	assert.Error(t, err)
}

func TestParseTokenRejectUnexpectedAlg(t *testing.T) {
	type CustomData struct {
		Role string `json:"role"`
	}

	claims := &Claims[CustomData]{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "user123",
			Issuer:    "example.com",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
		CustomData: CustomData{Role: "admin"},
	}
	unsafeToken := jwt.NewWithClaims(jwt.SigningMethodHS384, claims)
	tokenStr, err := unsafeToken.SignedString([]byte("secret"))
	require.NoError(t, err)

	auth, err := New[CustomData]("secret")
	require.NoError(t, err)
	_, err = auth.Parse(tokenStr)
	assert.Error(t, err)
}

func TestIssueWithAudience(t *testing.T) {
	type CustomData struct {
		Role string `json:"role"`
	}

	auth, err := New[CustomData]("secret")
	require.NoError(t, err)

	token, err := auth.Issue(
		"user123",
		"example.com",
		time.Now().Add(time.Hour),
		CustomData{Role: "admin"},
		WithAudience[CustomData]("web", "mobile"),
	)
	require.NoError(t, err)

	claims, err := auth.Parse(token)
	require.NoError(t, err)

	assert.Equal(t, jwt.ClaimStrings{"web", "mobile"}, claims.Audience)
}
