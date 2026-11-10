package ginupload

import (
	"os"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestUploadRequest_DocsMatchBinding 锁定「swagger 文档 required == binding 校验」。
//
// /files 的 handler 是流式 multipart 解析，不经过 ShouldBind，所以文档里的字段名与
// 必填完全由 uploadRequest 的 form/binding tag 推导：swaggo 只有在注解写成
// 「formData + struct」形式时才会读取 binding:"required" 并生成 required:true；
// 一旦退回手写 @Param content_hash formData string false，文档就会与运行时的
// content_hash is required 矛盾（历史缺陷）。
func TestUploadRequest_DocsMatchBinding(t *testing.T) {
	field, ok := reflect.TypeOf(uploadRequest{}).FieldByName("ContentHash")
	require.True(t, ok, "uploadRequest 必须保留 ContentHash 字段作为文档契约")
	require.Equal(t, "content_hash", field.Tag.Get("form"), "表单字段名由 form tag 决定")
	require.Contains(t, field.Tag.Get("binding"), "required", "binding 必须 required，与 handler 校验一致")

	source, err := os.ReadFile("upload_handler.go")
	require.NoError(t, err)
	require.Contains(t, string(source), "@Param req formData uploadRequest true",
		"注解必须引用文档 DTO，swaggo 据此把 binding required 写入 docs")
	require.NotContains(t, string(source), "@Param content_hash formData",
		"禁止在注解里手写 content_hash，避免出现第二个必填来源")
}
