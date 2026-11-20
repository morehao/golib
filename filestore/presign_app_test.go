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

// 本文件覆盖多业务线隔离的两条核心回归：
//  1. 跨 app 同内容各存一份，不跨 app 共享去重（含物理对象各自独立）；
//  2. 预签名 token 绑定 app，跨 app 转发/篡改 app 一律拒绝。

// newLocalSignedFileStore 构造带签名密钥与 BaseURL 的 local 驱动 FileStore，
// 以便签发真实（可被 storage 校验）的预签名 token。
func newLocalSignedFileStore(t *testing.T) *FileStore {
	t.Helper()
	st, err := storage.New(storage.DriverLocal, storage.Config{
		BaseDir:    t.TempDir(),
		BaseURL:    "http://files.local",
		SignSecret: tokenTestSecret,
	})
	require.NoError(t, err)
	fs, err := New(newTestDB(t), st, localTestBucket, WithAppScope(), WithSignSecret(tokenTestSecret))
	require.NoError(t, err)
	return fs
}

// TestCrossApp_SameContent_NotSharedDedup 跨 app 上传相同内容：
// 去重收敛到 app 内——各 app 得到各自的 FileEntity（不同 FileID）与各自的物理对象
// （不同 StorageURI / 各 app 前缀），互不共享、删一个不影响另一个。
func TestCrossApp_SameContent_NotSharedDedup(t *testing.T) {
	db := newTestDB(t)
	mock := &mockStorage{}
	fs, err := New(db, mock, "test-bucket", WithAppScope())
	require.NoError(t, err)

	content := "identical-bytes"
	hash := sha256Of(content)
	ctxA := testCtxForApp("app-a")
	ctxB := testCtxForApp("app-b")

	putA := func() *FileDetail {
		mock.putCalled = false
		d, e := fs.UploadAndRecord(ctxA, UploadAndRecordRequest{
			ContentHash: hash, Name: "a.txt", Size: int64(len(content)), Reader: strings.NewReader(content),
		})
		require.NoError(t, e)
		require.True(t, mock.putCalled, "app-a 首次上传应真正写入对象")
		return d
	}
	putB := func() *FileDetail {
		mock.putCalled = false
		d, e := fs.UploadAndRecord(ctxB, UploadAndRecordRequest{
			ContentHash: hash, Name: "b.txt", Size: int64(len(content)), Reader: strings.NewReader(content),
		})
		require.NoError(t, e)
		require.True(t, mock.putCalled, "app-b 即便内容相同也必须各存一份（不跨 app 共享去重）")
		return d
	}

	da := putA()
	db1 := putB()

	// 物理文件记录各自独立（不同 FileID / 不同 StorageURI）。
	require.NotEqual(t, da.FileID, db1.FileID, "跨 app 同内容必须各自独立的 FileEntity")
	require.NotEqual(t, da.StorageURI, db1.StorageURI, "跨 app 同内容必须各自独立的物理对象")
	require.Contains(t, da.StorageURI, "/app-a/", "app-a 的对象应落在 app-a 前缀下")
	require.Contains(t, db1.StorageURI, "/app-b/", "app-b 的对象应落在 app-b 前缀下")

	// 各自 app 内的 CheckExist 只命中自己的文件；同 app 内第二次才命中去重。
	hitA, ok, e := fs.CheckExist(ctxA, hash)
	require.NoError(t, e)
	require.True(t, ok)
	require.Equal(t, da.FileUploadID, hitA.FileUploadID, "app-a 的 CheckExist 应命中 app-a 自己的记录")

	mock.putCalled = false
	againA, e := fs.UploadAndRecord(ctxA, UploadAndRecordRequest{
		ContentHash: hash, Name: "a2.txt", Size: int64(len(content)), Reader: strings.NewReader(content),
	})
	require.NoError(t, e)
	require.False(t, mock.putCalled, "同一 app 内同内容第二次上传应命中本 app 去重，不重复写对象")
	require.Equal(t, da.FileID, againA.FileID, "同一 app 内应复用本 app 的物理文件")

	// 删除 app-a 的文件，app-b 的仍应存在（不共享）。
	require.NoError(t, fs.DeleteFile(ctxA, againA.FileUploadID))
	require.NoError(t, fs.DeleteFile(ctxA, da.FileUploadID))
	fhB, e := fs.fileDao.GetByID(ctxB, db1.FileID)
	require.NoError(t, e)
	require.NotNil(t, fhB, "删除 app-a 的对象不得影响 app-b 的物理文件")
}

// TestPresign_CrossApp_Rejected 预签名 token 绑 app：
// app-a 上下文签发的 URL，用 app-b 上下文消费必须被拒绝（防跨 app 转发越权读写）。
func TestPresign_CrossApp_Rejected(t *testing.T) {
	fs := newLocalSignedFileStore(t)
	ctxA := testCtxForApp("app-a")
	ctxB := testCtxForApp("app-b")

	detail, err := fs.UploadAndRecord(ctxA, UploadAndRecordRequest{
		ContentHash: sha256Of("hello"), Name: "h.txt", Size: 5, Reader: strings.NewReader("hello"),
	})
	require.NoError(t, err)

	presigned, err := fs.PresignGetFileURL(ctxA, detail.FileUploadID)
	require.NoError(t, err)

	u, err := url.Parse(presigned.URL)
	require.NoError(t, err)
	q := u.Query()
	token := q.Get("token")
	expires := q.Get("expires")
	require.NotEmpty(t, token, "premisign URL 应带 token")
	// 带上 app 绑定后 token 应为 4 段（内层 2 段 + app 载荷 + app 签名）。
	require.Len(t, strings.Split(token, "."), 4, "token 应携带 app 绑定层: %s", token)

	_, bucket, key, err := fs.parseStorageURI(detail.StorageURI)
	require.NoError(t, err)

	// 同 app 消费：通过。
	payload, err := VerifyPresignedTokenForContext(ctxA, tokenTestSecret, bucket, key, storage.PresignOpGet, token, expires)
	require.NoError(t, err)
	require.Equal(t, storage.PresignOpGet, payload.Op)

	// 跨 app 消费：拒绝。
	_, err = VerifyPresignedTokenForContext(ctxB, tokenTestSecret, bucket, key, storage.PresignOpGet, token, expires)
	require.ErrorIs(t, err, ErrPresignAppMismatch, "app-b 上下文消费 app-a 的 URL 必须被拒绝")

	// 无 app 上下文消费（生产端未绑定 app 时视为不匹配）：拒绝。
	_, err = VerifyPresignedTokenForContext(context.Background(), tokenTestSecret, bucket, key, storage.PresignOpGet, token, expires)
	require.ErrorIs(t, err, ErrPresignAppMismatch)
}

// TestPresign_TamperApp_Rejected 篡改 token 里的 app 字段（把 app-a 改成 app-b）
// 必须被拒绝：app 参与 HMAC，重算签名不符。
func TestPresign_TamperApp_Rejected(t *testing.T) {
	fs := newLocalSignedFileStore(t)
	ctxA := testCtxForApp("app-a")

	detail, err := fs.UploadAndRecord(ctxA, UploadAndRecordRequest{
		ContentHash: sha256Of("tamper"), Name: "t.txt", Size: 6, Reader: strings.NewReader("tamper"),
	})
	require.NoError(t, err)
	presigned, err := fs.PresignGetFileURL(ctxA, detail.FileUploadID)
	require.NoError(t, err)

	u, err := url.Parse(presigned.URL)
	require.NoError(t, err)
	q := u.Query()
	token := q.Get("token")
	expires := q.Get("expires")
	_, bucket, key, err := fs.parseStorageURI(detail.StorageURI)
	require.NoError(t, err)

	// 把 app 绑定载荷段替换为 app-b（不重算签名）。
	segs := strings.Split(token, ".")
	require.Len(t, segs, 4)
	tampered := encodeAppBoundTokenRaw(t, segs[0]+"."+segs[1], "app-b")

	// 用 app-b 上下文消费被篡改的 token：app 字段与 ctx 相符，但签名不符 → 拒绝。
	_, err = VerifyPresignedTokenForContext(testCtxForApp("app-b"), tokenTestSecret, bucket, key, storage.PresignOpGet, tampered, expires)
	require.ErrorIs(t, err, ErrPresignAppMismatch, "篡改 app 字段必须因签名不符被拒绝")
}

// encodeAppBoundTokenRaw 只在测试中构造"未按正确 secret 签名"的 token，用于篡改场景。
// 直接复用 product 的 encodeAppBoundToken 但传入错误的 secret，模拟攻击者不知道密钥。
func encodeAppBoundTokenRaw(t *testing.T, inner, app string) string {
	t.Helper()
	tok, err := encodeAppBoundToken(inner, app, "attacker-unknown-secret")
	require.NoError(t, err)
	return tok
}
