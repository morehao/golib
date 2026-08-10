package jwtauth

import (
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mustRSAKey(t *testing.T) (*rsa.PrivateKey, *rsa.PublicKey) {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	return priv, &priv.PublicKey
}

func TestRS256IssueAndParse(t *testing.T) {
	type CustomData struct {
		Role string `json:"role"`
	}

	priv, pub := mustRSAKey(t)
	signer, err := NewRS256Signer(priv, pub)
	require.NoError(t, err)
	auth, err := New[CustomData](signer)
	require.NoError(t, err)

	token, err := auth.Issue(
		"user123",
		"example.com",
		time.Now().Add(time.Hour),
		CustomData{Role: "admin"},
		WithAudience[CustomData]("web"),
		WithID[CustomData]("id-rs256"),
	)
	require.NoError(t, err)

	claims, err := auth.Parse(token)
	require.NoError(t, err)
	assert.Equal(t, "admin", claims.CustomData.Role)
	assert.Equal(t, "user123", claims.Subject)
	assert.Equal(t, "example.com", claims.Issuer)
	assert.Equal(t, "id-rs256", claims.ID)
	assert.Equal(t, jwt.ClaimStrings{"web"}, claims.Audience)
}

func TestRS256VerifierOnly(t *testing.T) {
	type CustomData struct {
		Role string `json:"role"`
	}

	priv, pub := mustRSAKey(t)
	issuer, err := New[CustomData](mustRS256Signer(t, priv, pub))
	require.NoError(t, err)
	token, err := issuer.Issue("user123", "example.com", time.Now().Add(time.Hour), CustomData{Role: "admin"})
	require.NoError(t, err)

	// 下游服务仅持公钥即可验签。
	verifier, err := NewRS256Verifier(pub)
	require.NoError(t, err)
	verifyAuth, err := New[CustomData](verifier)
	require.NoError(t, err)

	claims, err := verifyAuth.Parse(token)
	require.NoError(t, err)
	assert.Equal(t, "admin", claims.CustomData.Role)

	// 只验签实例不应具备签发能力。
	_, err = verifyAuth.Issue("user1", "example.com", time.Now().Add(time.Hour), CustomData{Role: "admin"})
	assert.True(t, errors.Is(err, ErrNotSignable))

	// 篡改载荷后验签应失败（仅替换末字符即可破坏签名）。
	tampered := token[:len(token)-5] + "xxxyz"
	_, err = verifyAuth.Parse(tampered)
	assert.Error(t, err)
}

func TestRS256ConstructorValidation(t *testing.T) {
	priv, pub := mustRSAKey(t)

	_, err := NewRS256Signer(nil, pub)
	assert.True(t, errors.Is(err, ErrNilRSAPrivateKey))

	_, err = NewRS256Signer(priv, nil)
	assert.True(t, errors.Is(err, ErrNilRSAPublicKey))

	_, err = NewRS256Verifier(nil)
	assert.True(t, errors.Is(err, ErrNilRSAPublicKey))
}

func TestParseRejectsAlgorithmConfusionRS256(t *testing.T) {
	type CustomData struct {
		Role string `json:"role"`
	}

	priv, pub := mustRSAKey(t)
	rs256Auth, err := New[CustomData](mustRS256Signer(t, priv, pub))
	require.NoError(t, err)

	// 用 HS256 签发的 token 喂给 RS256 实例必须拒绝。
	hs256Auth, err := New[CustomData](mustHS256(t, "secret"))
	require.NoError(t, err)
	hsToken, err := hs256Auth.Issue("user123", "example.com", time.Now().Add(time.Hour), CustomData{Role: "admin"})
	require.NoError(t, err)

	_, err = rs256Auth.Parse(hsToken)
	assert.Error(t, err)
}

func mustRS256Signer(t *testing.T, priv *rsa.PrivateKey, pub *rsa.PublicKey) *RS256Signer {
	t.Helper()
	signer, err := NewRS256Signer(priv, pub)
	require.NoError(t, err)
	return signer
}
