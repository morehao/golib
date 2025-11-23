package jwtauth

import (
	"testing"
	"time"

	"github.com/morehao/golib/gutil"
	"github.com/stretchr/testify/assert"
)

func TestCreateToken(t *testing.T) {
	type CustomData struct {
		Role string `json:"role"`
	}

	signKey := "secret"
	uuid := "123456"
	now := time.Now()
	expiresAt := time.Now().Add(24 * time.Hour)

	// 使用新的泛型 API
	claims := NewClaims(
		"user123",                             // subject (必填)
		expiresAt,                             // expiresAt (必填)
		CustomData{Role: "admin"},             // customData (必填)
		WithIssuer[CustomData]("example.com"), // 可选
		WithAudience[CustomData]("audience1", "audience2"), // 可选
		WithNotBefore[CustomData](now),                     // 可选
		WithID[CustomData](uuid),                           // 可选
	)

	token, err := CreateToken(signKey, claims)
	assert.Nil(t, err)
	t.Log(token)
}

func TestParseToken(t *testing.T) {
	// 先创建一个 token
	type CustomData struct {
		CompanyId uint64 `json:"companyId"`
		Role      string `json:"role"`
	}

	signKey := "secret"
	expiresAt := time.Now().Add(24 * time.Hour)

	// 创建 token
	claims := NewClaims(
		"user123",
		expiresAt,
		CustomData{CompanyId: 1001, Role: "admin"},
		WithIssuer[CustomData]("example.com"),
	)

	token, err := CreateToken(signKey, claims)
	assert.Nil(t, err)
	t.Log("Created token:", token)

	// 解析 token
	var parsedClaims Claims[CustomData]
	err = ParseToken(signKey, token, &parsedClaims)
	assert.Nil(t, err)
	t.Log(gutil.ToJsonString(parsedClaims))
	t.Log("Role:", parsedClaims.CustomData.Role)
	t.Log("CompanyId:", parsedClaims.CustomData.CompanyId)

	// 验证数据
	assert.Equal(t, "admin", parsedClaims.CustomData.Role)
	assert.Equal(t, uint64(1001), parsedClaims.CustomData.CompanyId)
	assert.Equal(t, "user123", parsedClaims.Subject)
}

func TestRenewToken(t *testing.T) {
	type CustomData struct {
		Role string `json:"role"`
	}

	signKey := "secret"
	expiresAt := time.Now().Add(1 * time.Hour)

	// 创建原始 token
	claims := NewClaims(
		"user123",
		expiresAt,
		CustomData{Role: "admin"},
		WithIssuer[CustomData]("example.com"),
		WithID[CustomData]("123456"),
	)

	token, err := CreateToken(signKey, claims)
	assert.Nil(t, err)
	t.Log("Original token:", token)

	// 续期 token
	newExpirationTime := 2 * time.Hour
	newToken, err := RenewToken(signKey, token, newExpirationTime, CustomData{})
	assert.Nil(t, err)
	t.Log("Renewed token:", newToken)

	// 验证新 token
	var newClaims Claims[CustomData]
	err = ParseToken(signKey, newToken, &newClaims)
	assert.Nil(t, err)
	t.Log(gutil.ToJsonString(newClaims))

	// 验证数据保留
	assert.Equal(t, "admin", newClaims.CustomData.Role)
	assert.Equal(t, "user123", newClaims.Subject)
	assert.Equal(t, "example.com", newClaims.Issuer)
}
