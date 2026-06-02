# filestore 预签名有效期可选实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为 `PresignGetFileURL` 和 `PresignUploadPartURL` 添加可选有效期支持，默认值使用包级常量，调用方可通过选项模式覆盖。

**Architecture:** 新增 `PresignOption` 和 `FileStoreOption` 选项类型，`WithExpires` 选项函数。包级常量 `defaultPresignExpiry` 作为默认值。

**Tech Stack:** Go

---

### Task 1: 添加选项类型和默认常量

**Files:**
- Modify: `filestore/filestore.go`

- [ ] **Step 1: 添加常量、选项类型和辅助函数**

```go
const defaultPresignExpiry = 24 * time.Hour

type PresignOption func(*presignOptions)

type presignOptions struct {
    expires time.Duration
}

func WithExpires(d time.Duration) PresignOption {
    return func(o *presignOptions) {
        o.expires = d
    }
}

type FileStoreOption func(*fileStoreOptions)

type fileStoreOptions struct{}

func applyPresignOptions(opts ...PresignOption) time.Duration {
    var o presignOptions
    for _, fn := range opts {
        fn(&o)
    }
    if o.expires > 0 {
        return o.expires
    }
    return defaultPresignExpiry
}
```

- [ ] **Step 2: 修改 New 签名**

```go
func New(db *gorm.DB, st spec.Storage, opts ...FileStoreOption) (*FileStore, error) {
    var o fileStoreOptions
    for _, fn := range opts {
        fn(&o)
    }

    if err := db.AutoMigrate(&FileRecord{}); err != nil {
        return nil, fmt.Errorf("filestore.New: auto-migrate: %w", err)
    }
    return &FileStore{store: newStore(db), st: st}, nil
}
```

- [ ] **Step 3: 修改 PresignGetFileURL 和 PresignUploadPartURL 签名**

```go
func (s *FileStore) PresignGetFileURL(ctx context.Context, id uint, opts ...PresignOption) (string, error) {
    rec, err := s.store.GetByID(ctx, id)
    if err != nil {
        return "", fmt.Errorf("filestore.PresignGetFileURL: %w", err)
    }

    expires := applyPresignOptions(opts...)
    url, err := s.st.PresignGetURL(ctx, rec.StoragePath, expires)
    if err != nil {
        return "", fmt.Errorf("filestore.PresignGetFileURL: %w", err)
    }
    return url, nil
}

func (s *FileStore) PresignUploadPartURL(ctx context.Context, id uint, partNum int32, opts ...PresignOption) (string, error) {
    rec, err := s.store.GetByID(ctx, id)
    if err != nil {
        return "", fmt.Errorf("filestore.PresignUploadPartURL: %w", err)
    }
    if rec.UploadID == "" {
        return "", fmt.Errorf("%w: id=%d", ErrNotMultipartUpload, id)
    }

    uploader, err := s.st.GetMultipartUploader(ctx, rec.StoragePath, rec.UploadID)
    if err != nil {
        return "", fmt.Errorf("filestore.PresignUploadPartURL: get uploader: %w", err)
    }

    expires := applyPresignOptions(opts...)
    url, err := uploader.PresignUploadPartURL(ctx, partNum, expires)
    if err != nil {
        return "", fmt.Errorf("filestore.PresignUploadPartURL: presign: %w", err)
    }
    return url, nil
}
```

- [ ] **Step 4: 编译验证**

Run: `go build ./filestore/...`

### Task 2: 更新测试

**Files:**
- Modify: `filestore/filestore_test.go`

- [ ] **Step 1: 更新现有测试方法签名**

将所有 `PresignGetFileURL(ctx, id, time.Hour)` 改为 `PresignGetFileURL(ctx, id, WithExpires(time.Hour))`
将所有 `PresignUploadPartURL(ctx, id, 1, time.Hour)` 改为 `PresignUploadPartURL(ctx, id, 1, WithExpires(time.Hour))`

- [ ] **Step 2: 添加默认有效期测试**

```go
func TestPresignGetFileURL_DefaultExpiry(t *testing.T) {
    db := newTestDB(t)
    mock := &mockStorage{}
    fs, err := New(db, mock)
    require.NoError(t, err)

    rec, err := fs.RecordUpload(context.Background(), RecordUploadRequest{
        Fingerprint: "default-expiry",
        Name:        "test.txt",
        Size:        100,
        MimeType:    "text/plain",
        StoragePath: "files/test.txt",
    })
    require.NoError(t, err)

    url, err := fs.PresignGetFileURL(context.Background(), rec.ID)
    require.NoError(t, err)
    require.True(t, mock.presignGetURLCalled)
    require.Contains(t, url, defaultPresignExpiry.String())
}
```

- [ ] **Step 3: 添加 WithExpires 覆盖测试**

```go
func TestPresignUploadPartURL_WithExpires(t *testing.T) {
    db := newTestDB(t)
    fs, err := New(db, &mockStorage{})
    require.NoError(t, err)

    rec, err := fs.InitMultipartUpload(context.Background(), InitMultipartUploadRequest{
        Fingerprint: "presign-expires-test",
        Name:        "test.mp4",
        Size:        1000,
        StoragePath: "test.mp4",
    })
    require.NoError(t, err)

    url, err := fs.PresignUploadPartURL(context.Background(), rec.ID, 1, WithExpires(5*time.Minute))
    require.NoError(t, err)
    require.Contains(t, url, "5m0s")
}
```

- [ ] **Step 4: 运行测试**

Run: `go test ./filestore/... -v`
Expected: ALL PASS

- [ ] **Step 5: 提交**

```bash
git add -A && git commit -m "feat(filestore): add optional presign expiry with options pattern"
```
