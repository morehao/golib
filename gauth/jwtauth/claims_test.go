package jwtauth

import (
	"testing"
	"time"

	"github.com/morehao/golib/gutil"
)

func TestNewClaims(t *testing.T) {
	// 自定义 claims 结构体
	type CustomData struct {
		Role string `json:"role"`
	}

	// 使用泛型版本的 NewClaims
	// 必填参数：subject, expiresAt, customData
	// 可选参数：Issuer, Audience, NotBefore, ID
	expiresAt := time.Now().Add(24 * time.Hour)
	notBefore := time.Now()

	claims := NewClaims(
		"user123",                             // subject (必填)
		expiresAt,                             // expiresAt (必填)
		CustomData{Role: "admin"},             // customData (必填)
		WithIssuer[CustomData]("example.com"), // 可选
		WithAudience[CustomData]("audience1", "audience2"), // 可选
		WithNotBefore[CustomData](notBefore),               // 可选
		WithID[CustomData]("unique-id-12345"),              // 可选
	)

	t.Log(gutil.ToJsonString(claims))
}

// TestNewClaimsSimple 测试简化版本（只使用必填参数）
func TestNewClaimsSimple(t *testing.T) {
	type UserInfo struct {
		UserID   uint64 `json:"userId"`
		Username string `json:"username"`
	}

	claims := NewClaims(
		"user456",
		time.Now().Add(2*time.Hour),
		UserInfo{
			UserID:   456,
			Username: "john_doe",
		},
	)

	t.Log(gutil.ToJsonString(claims))
}

// TestNewClaimsEmpty 测试空自定义数据
func TestNewClaimsEmpty(t *testing.T) {
	// 使用空结构体作为类型参数
	type EmptyData struct{}

	claims := NewClaims(
		"user789",
		time.Now().Add(1*time.Hour),
		EmptyData{},
		WithIssuer[EmptyData]("my-service"),
	)

	t.Log(gutil.ToJsonString(claims))
}
