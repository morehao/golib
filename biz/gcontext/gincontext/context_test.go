package gincontext

import (
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/morehao/golib/biz/gcontext"
)

func setUintIDs(c *gin.Context) {
	c.Set(gcontext.KeyPersonID, uint(11))
	c.Set(gcontext.KeyUserID, uint(22))
	c.Set(gcontext.KeyOrgID, uint(33))
	c.Set(gcontext.KeyTenantID, uint(44))
	c.Set(gcontext.KeyDeptID, uint(55))
	c.Set(gcontext.KeyUserType, "admin")
}

func setStringIDs(c *gin.Context) {
	c.Set(gcontext.KeyPersonID, "11")
	c.Set(gcontext.KeyUserID, "22")
	c.Set(gcontext.KeyOrgID, "33")
	c.Set(gcontext.KeyTenantID, "44")
	c.Set(gcontext.KeyDeptID, "55")
	c.Set(gcontext.KeyUserType, "admin")
}

// TestIDGettersCompat 验证无论存储 uint 还是 string，uint 版与 string 版 Getter 都能正确取值。
func TestIDGettersCompat(t *testing.T) {
	cases := []struct {
		name    string
		setter  func(c *gin.Context)
		uintVal uint
	}{
		{"uint", setUintIDs, 1},
		{"string", setStringIDs, 1},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			c := &gin.Context{}
			tc.setter(c)

			assertEqual(t, "GetPersonID", GetPersonID(c), uint(11))
			assertEqual(t, "GetPersonIDString", GetPersonIDString(c), "11")
			assertEqual(t, "GetUserID", GetUserID(c), uint(22))
			assertEqual(t, "GetUserIDString", GetUserIDString(c), "22")
			assertEqual(t, "GetOrgID", GetOrgID(c), uint(33))
			assertEqual(t, "GetOrgIDString", GetOrgIDString(c), "33")
			assertEqual(t, "GetTenantID", GetTenantID(c), uint(44))
			assertEqual(t, "GetTenantIDString", GetTenantIDString(c), "44")
			assertEqual(t, "GetDeptID", GetDeptID(c), uint(55))
			assertEqual(t, "GetDeptIDString", GetDeptIDString(c), "55")
			assertEqual(t, "GetUserType", GetUserType(c), "admin")
		})
	}
}

// TestGenericGettersCompat 验证通用 GetUint/GetUint64/GetString 对 string/uint 存储均兼容。
func TestGenericGettersCompat(t *testing.T) {
	cases := []struct {
		name   string
		setter func(c *gin.Context)
	}{
		{"uint", setUintIDs},
		{"string", setStringIDs},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			c := &gin.Context{}
			tc.setter(c)

			assertEqual(t, "GetUint", GetUint(c, gcontext.KeyUserID), uint(22))
			assertEqual(t, "GetUint64", GetUint64(c, gcontext.KeyUserID), uint64(22))
			assertEqual(t, "GetString", GetString(c, gcontext.KeyUserID), "22")
		})
	}
}

func assertEqual[T comparable](t *testing.T, name string, got, want T) {
	t.Helper()
	if got != want {
		t.Fatalf("%s: got %v, want %v", name, got, want)
	}
}
