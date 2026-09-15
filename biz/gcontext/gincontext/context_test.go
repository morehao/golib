package gincontext

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/morehao/golib/biz/gcontext"
	"github.com/morehao/golib/gerror"
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

// TestAppErrorContext 验证渲染层写出的业务错误能被访问日志这类横切组件读到，
// 而不必去解析（可能被压缩或截断的）响应体。
func TestAppErrorContext(t *testing.T) {
	// 未写入时为空值
	if code, msg := GetAppError(&gin.Context{}); code != 0 || msg != "" {
		t.Fatalf("未写入时 GetAppError() = (%d, %q), want (0, \"\")", code, msg)
	}

	t.Run("gerror 业务错误", func(t *testing.T) {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		Fail(c, gerror.Error{Code: 1234, Msg: "业务失败"})

		code, msg := GetAppError(c)
		assertEqual(t, "code", code, 1234)
		assertEqual(t, "msg", msg, "业务失败")
		if !strings.Contains(rec.Body.String(), `"code":1234`) {
			t.Fatalf("envelope 响应体应包含业务错误码: %s", rec.Body.String())
		}
	})

	t.Run("普通错误", func(t *testing.T) {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		Fail(c, errors.New("boom"))

		code, msg := GetAppError(c)
		assertEqual(t, "code", code, -1)
		assertEqual(t, "msg", msg, "boom")
	})
}

func assertEqual[T comparable](t *testing.T, name string, got, want T) {
	t.Helper()
	if got != want {
		t.Fatalf("%s: got %v, want %v", name, got, want)
	}
}
