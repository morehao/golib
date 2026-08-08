package jwtauth

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mustECKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	return key
}

func TestES256IssueAndParse(t *testing.T) {
	type CustomData struct {
		Role string `json:"role"`
	}

	priv := mustECKey(t)
	signer, err := NewES256Signer(priv)
	require.NoError(t, err)
	auth, err := New[CustomData](signer)
	require.NoError(t, err)

	token, err := auth.Issue(
		"user123",
		"example.com",
		time.Now().Add(time.Hour),
		CustomData{Role: "admin"},
		WithAudience[CustomData]("web"),
		WithID[CustomData]("id-es256"),
	)
	require.NoError(t, err)

	claims, err := auth.Parse(token)
	require.NoError(t, err)
	assert.Equal(t, "admin", claims.CustomData.Role)
	assert.Equal(t, "user123", claims.Subject)
	assert.Equal(t, "example.com", claims.Issuer)
	assert.Equal(t, "id-es256", claims.ID)
	assert.Equal(t, jwt.ClaimStrings{"web"}, claims.Audience)
}

func TestES256VerifierOnly(t *testing.T) {
	type CustomData struct {
		Role string `json:"role"`
	}

	priv := mustECKey(t)
	issuer, err := New[CustomData](mustES256Signer(t, priv))
	require.NoError(t, err)
	token, err := issuer.Issue("user123", "example.com", time.Now().Add(time.Hour), CustomData{Role: "admin"})
	require.NoError(t, err)

	verifier, err := NewES256Verifier(&priv.PublicKey)
	require.NoError(t, err)
	verifyAuth, err := New[CustomData](verifier)
	require.NoError(t, err)

	claims, err := verifyAuth.Parse(token)
	require.NoError(t, err)
	assert.Equal(t, "admin", claims.CustomData.Role)

	// 只验签实例不应具备签发能力。
	_, err = verifyAuth.Issue("user1", "example.com", time.Now().Add(time.Hour), CustomData{Role: "admin"})
	assert.True(t, errors.Is(err, ErrNotSignable))

	tampered := token[:len(token)-5] + "xxxyz"
	_, err = verifyAuth.Parse(tampered)
	assert.Error(t, err)
}

func TestES256ConstructorValidation(t *testing.T) {
	_, err := NewES256Signer(nil)
	assert.True(t, errors.Is(err, ErrNilECDSAPrivateKey))

	_, err = NewES256Verifier(nil)
	assert.True(t, errors.Is(err, ErrNilECDSAPublicKey))

	// 非 P-256 曲线必须拒绝。
	p384Key, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	require.NoError(t, err)
	_, err = NewES256Signer(p384Key)
	assert.True(t, errors.Is(err, ErrUnsupportedCurve))

	_, err = NewES256Verifier(&p384Key.PublicKey)
	assert.True(t, errors.Is(err, ErrUnsupportedCurve))
}

func TestParseRejectsAlgorithmConfusionES256(t *testing.T) {
	type CustomData struct {
		Role string `json:"role"`
	}

	es256Auth, err := New[CustomData](mustES256Signer(t, mustECKey(t)))
	require.NoError(t, err)

	// 用 HS256 签发的 token 喂给 ES256 实例必须拒绝。
	hs256Auth, err := New[CustomData](mustHS256(t, "secret"))
	require.NoError(t, err)
	hsToken, err := hs256Auth.Issue("user123", "example.com", time.Now().Add(time.Hour), CustomData{Role: "admin"})
	require.NoError(t, err)

	_, err = es256Auth.Parse(hsToken)
	assert.Error(t, err)
}

func mustES256Signer(t *testing.T, priv *ecdsa.PrivateKey) *ES256Signer {
	t.Helper()
	signer, err := NewES256Signer(priv)
	require.NoError(t, err)
	return signer
}
