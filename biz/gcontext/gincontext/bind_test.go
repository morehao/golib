package gincontext

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// testReq 模拟「path ID + body 必填字段」的混合请求结构体，
// 即 REST 路由下 PUT /xxx/:xxxID 的标准形态。
type testReq struct {
	UserID uint   `json:"userID" uri:"userID" binding:"required"`
	Name   string `json:"name" binding:"required"`
}

func TestBindPathParamsThenJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.PUT("/users/:userID", func(c *gin.Context) {
		var req testReq
		if err := BindPathParams(c, &req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"userID": req.UserID, "name": req.Name})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPut, "/users/42", strings.NewReader(`{"name":"alice"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["userID"] != float64(42) || resp["name"] != "alice" {
		t.Fatalf("unexpected resp: %v", resp)
	}
}

func TestBindPathParamsInvalidNumber(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.PUT("/users/:userID", func(c *gin.Context) {
		var req testReq
		if err := BindPathParams(c, &req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"userID": req.UserID})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPut, "/users/abc", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for non-numeric path param, got %d", w.Code)
	}
}

func TestBindPathParamsThenQuery(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/users/:userID/identities", func(c *gin.Context) {
		var req struct {
			UserID uint `uri:"userID" binding:"required"`
			Page   int  `form:"page"`
		}
		if err := BindPathParams(c, &req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		if err := c.ShouldBindQuery(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"userID": req.UserID, "page": req.Page})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/users/7/identities?page=2", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["userID"] != float64(7) || resp["page"] != float64(2) {
		t.Fatalf("unexpected resp: %v", resp)
	}
}

// TestNativeShouldBindUriFailsWithBodyRequiredField 证明 gin 原生 ShouldBindUri
// 在「path ID + body 必填字段」结构体上会误伤（body 字段尚未绑定即校验失败），
// 这正是 BindPathParams 存在的原因。
func TestNativeShouldBindUriFailsWithBodyRequiredField(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.PUT("/users/:userID", func(c *gin.Context) {
		var req testReq
		if err := c.ShouldBindUri(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPut, "/users/42", strings.NewReader(`{"name":"alice"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code == http.StatusOK {
		t.Fatal("expected gin native ShouldBindUri to fail when body-required field present")
	}
}
