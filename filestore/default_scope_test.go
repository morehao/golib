package filestore

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"github.com/morehao/golib/storage"
	_ "github.com/morehao/golib/storage/driver/local"
	"github.com/stretchr/testify/require"
)

// 本文件验证"默认不隔离"语义：不传 WithAppScope 时 filestore 是单租户组件，
// 无需任何 app 上下文即可上传/查询，object key 不含 app 段，预签名 URL 不绑 app。
// 这是与"opt-in 才隔离"设计对齐的关键回归——默认单 app 零负担。

// TestDefault_NoAppScope_UploadWorksWithoutApp 默认 New（无 WithAppScope）：
// 用纯粹的 context.Background()（无 app）也能上传与查询，object key 形如
// files/<id[:2]>/<id>（无 app 段），且 app_id 落库为 ”。
func TestDefault_NoAppScope_UploadWorksWithoutApp(t *testing.T) {
	db := newTestDB(t)
	mock := &mockStorage{}
	fs, err := New(db, mock, "test-bucket") // 不传 WithAppScope
	require.NoError(t, err)
	require.False(t, fs.appScope, "默认应为非隔离")

	ctx := context.Background() // 无 app 上下文

	detail, err := fs.UploadAndRecord(ctx, UploadAndRecordRequest{
		ContentHash: "default-hash",
		Name:        "photo.jpg",
		Size:        4,
		Reader:      strings.NewReader("data"),
	})
	require.NoError(t, err)
	require.NotNil(t, detail)
	require.True(t, mock.putCalled, "默认模式下应真正写入对象")

	// key 形如 files/<id[:2]>/<id>：无 app 段。
	key := mock.lastKey
	require.True(t, strings.HasPrefix(key, objectKeyPrefix), "key 必须带 files/ 前缀: %s", key)
	rest := strings.TrimPrefix(key, objectKeyPrefix)
	parts := strings.SplitN(rest, "/", 2)
	require.Len(t, parts, 2, "key 应形如 files/<id[:2]>/<id>（无 app 段）: %s", key)
	require.Len(t, parts[0], 2, "第一段应为 ID 前两位，而非 app 名: %s", key)
	require.Equal(t, parts[1][:2], parts[0], "目录名必须等于 ID 前两位: %s", key)
	require.Len(t, parts[1], 36, "ID 应为 UUID(v7) 字符串: %s", parts[1])
	require.NotContains(t, key, testApp, "非隔离模式的 key 不应出现 app 段")

	// 落库的 app_id 应为空串（未注册租户回调，Create 不补 app_id）。
	require.True(t, fs.fileDao != nil)
	fh, err := fs.fileDao.GetByCond(ctx, &fileCond{ContentHash: "default-hash"})
	require.NoError(t, err)
	require.NotNil(t, fh)
	require.Empty(t, fh.AppID, "非隔离模式下 app_id 应为空串")

	// 无 app 也能按 id 查询（GetFile）。
	got, err := fs.GetFile(ctx, detail.FileUploadID)
	require.NoError(t, err)
	require.Equal(t, "photo.jpg", got.Name)
	require.Equal(t, detail.StorageURI, got.StorageURI)

	// 无 app 也能 CheckExist。
	hit, ok, err := fs.CheckExist(ctx, "default-hash")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, detail.FileUploadID, hit.FileUploadID)
}

// TestDefault_NoAppScope_PresignNotAppBound 默认 New（无 WithAppScope）：
// 预签名 URL 不绑 app（token 仍是 storage 原协议的 2 段），消费端无 app 上下文
// 也能正常校验通过。
func TestDefault_NoAppScope_PresignNotAppBound(t *testing.T) {
	st, err := storage.New(storage.DriverLocal, storage.Config{
		BaseDir:    t.TempDir(),
		BaseURL:    "http://files.local",
		SignSecret: tokenTestSecret,
	})
	require.NoError(t, err)
	fs, err := New(newTestDB(t), st, localTestBucket, WithSignSecret(tokenTestSecret)) // 不传 WithAppScope
	require.NoError(t, err)

	ctx := context.Background() // 无 app
	detail, err := fs.UploadAndRecord(ctx, UploadAndRecordRequest{
		ContentHash: sha256Of("hello-default"),
		Name:        "h.txt",
		Size:        13,
		Reader:      strings.NewReader("hello-default"),
	})
	require.NoError(t, err)

	presigned, err := fs.PresignGetFileURL(ctx, detail.FileUploadID)
	require.NoError(t, err)

	u, err := url.Parse(presigned.URL)
	require.NoError(t, err)
	q := u.Query()
	token := q.Get("token")
	expires := q.Get("expires")
	require.NotEmpty(t, token, "预签名 URL 应带 token")
	require.Len(t, strings.Split(token, "."), 2, "非隔离模式 token 不应携带 app 绑定（仍为 2 段）: %s", token)

	_, bucket, key, err := fs.parseStorageURI(detail.StorageURI)
	require.NoError(t, err)

	// 无 app 上下文消费：应通过（不绑 app）。
	payload, err := VerifyPresignedTokenForContext(ctx, tokenTestSecret, bucket, key, storage.PresignOpGet, token, expires)
	require.NoError(t, err)
	require.Equal(t, storage.PresignOpGet, payload.Op)
}
