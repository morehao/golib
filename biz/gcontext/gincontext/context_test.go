package gincontext

import (
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/morehao/golib/biz/gcontext"
)

func setStringIDs(c *gin.Context) {
	c.Set(gcontext.KeyPersonID, "11")
	c.Set(gcontext.KeyUserID, "22")
	c.Set(gcontext.KeyOrgID, "33")
	c.Set(gcontext.KeyTenantID, "44")
	c.Set(gcontext.KeyDeptID, "55")
	c.Set(gcontext.KeyUserType, "admin")
}

func setUUIDIDs(c *gin.Context) {
	c.Set(gcontext.KeyPersonID, "person-1111")
	c.Set(gcontext.KeyUserID, "user-2222")
	c.Set(gcontext.KeyOrgID, "org-3333")
	c.Set(gcontext.KeyTenantID, "tenant-4444")
	c.Set(gcontext.KeyDeptID, "dept-5555")
	c.Set(gcontext.KeyUserType, "admin")
}

// TestIDGettersString 验证数值字符串存储下，uint 版与 string 版 Getter 都能正确取值。
func TestIDGettersString(t *testing.T) {
	c := &gin.Context{}
	setStringIDs(c)

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
}

// TestIDGettersUUID 验证 UUID（非数字）存储下，string 版返回原值，uint 版返回 0。
func TestIDGettersUUID(t *testing.T) {
	c := &gin.Context{}
	setUUIDIDs(c)

	assertEqual(t, "GetPersonID", GetPersonID(c), uint(0))
	assertEqual(t, "GetPersonIDString", GetPersonIDString(c), "person-1111")
	assertEqual(t, "GetUserID", GetUserID(c), uint(0))
	assertEqual(t, "GetUserIDString", GetUserIDString(c), "user-2222")
	assertEqual(t, "GetOrgID", GetOrgID(c), uint(0))
	assertEqual(t, "GetOrgIDString", GetOrgIDString(c), "org-3333")
	assertEqual(t, "GetTenantID", GetTenantID(c), uint(0))
	assertEqual(t, "GetTenantIDString", GetTenantIDString(c), "tenant-4444")
	assertEqual(t, "GetDeptID", GetDeptID(c), uint(0))
	assertEqual(t, "GetDeptIDString", GetDeptIDString(c), "dept-5555")
	assertEqual(t, "GetUserType", GetUserType(c), "admin")
}

// TestGenericGettersString 验证通用 GetUint/GetUint64/GetString 对字符串存储均正确。
func TestGenericGettersString(t *testing.T) {
	c := &gin.Context{}
	setStringIDs(c)

	assertEqual(t, "GetUint", GetUint(c, gcontext.KeyUserID), uint(22))
	assertEqual(t, "GetUint64", GetUint64(c, gcontext.KeyUserID), uint64(22))
	assertEqual(t, "GetString", GetString(c, gcontext.KeyUserID), "22")
}

func assertEqual[T comparable](t *testing.T, name string, got, want T) {
	t.Helper()
	if got != want {
		t.Fatalf("%s: got %v, want %v", name, got, want)
	}
}
