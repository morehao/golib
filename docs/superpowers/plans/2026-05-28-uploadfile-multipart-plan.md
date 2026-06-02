# filestore multipart upload Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add S3 multipart upload support to filestore package with client-direct-to-S3 strategy.

**Architecture:** Extend `spec.Storage` interfaces with `PresignUploadPartURL` and `GetMultipartUploader`; add `UploadID`/`ChunkSize`/`StorageKey` to `FileRecord`; add 4 new methods to `FileStore`; refactor `store.go` condition builders.

**Tech Stack:** Go, GORM, spec.Storage

---

### Task 1: Add UploadID accessor to MultipartUploader + GetMultipartUploader to Storage

- [ ] **Step 1: Add `UploadID() string` to MultipartUploader + `GetMultipartUploader` to Storage**

Edit `storage/spec/contract.go`:

```go
type MultipartUploader interface {
    UploadID() string
    UploadPart(ctx context.Context, partNum int32, reader io.Reader, size int64) (Part, error)
    PresignUploadPartURL(ctx context.Context, partNum int32, expires time.Duration) (string, error)
    Complete(ctx context.Context, parts []Part) error
    Abort(ctx context.Context) error
    ListParts(ctx context.Context, opts ...ListPartsOption) (*ListPartsResult, error)
}

type Storage interface {
    // ... existing methods ...
    NewMultipartUpload(ctx context.Context, key string, opts ...MultipartOption) (MultipartUploader, error)
    GetMultipartUploader(ctx context.Context, key string, uploadID string) (MultipartUploader, error)
    // ...
}
```

- [ ] **Step 2: Verify `go build ./storage/spec/` passes**

### Task 2: Extend FileRecord with multipart fields and conditions

- [ ] **Step 1: Add `UploadID`, `ChunkSize`, `StorageKey` fields + `FileStatusMerging`**

Edit `filestore/model.go`:

```go
type FileRecord struct {
    gorm.Model
    Fingerprint string     `gorm:"column:fingerprint;type:varchar(64);uniqueIndex:uk_fingerprint;comment:文件指纹(SHA256)，用于秒传去重"`
    Name        string     `gorm:"column:name;type:varchar(256);comment:原始文件名"`
    Size        int64      `gorm:"column:size;comment:文件大小(字节)"`
    MimeType    string     `gorm:"column:mime_type;type:varchar(128);comment:MIME 类型"`
    StorageURI  string     `gorm:"column:storage_uri;type:varchar(512);comment:存储位置 URI，格式 {provider}://{bucket}/{key}"`
    StorageKey  string     `gorm:"column:storage_key;type:varchar(512);comment:存储对象 key"`
    UploadID    string     `gorm:"column:upload_id;type:varchar(128);index;comment:S3 multipart upload session ID"`
    ChunkSize   int64      `gorm:"column:chunk_size;comment:standard chunk size in bytes (0 for non-multipart)"`
    Status      FileStatus `gorm:"column:status;type:varchar(32);default:uploading;comment:状态"`
}

const FileStatusMerging FileStatus = "merging"
```

- [ ] **Step 2: Add `FingerprintCond` and `IDCond` condition types after `fileCond`**

```go
type FingerprintCond struct {
    Fingerprint string
    Status      FileStatus
}

func (c *FingerprintCond) BuildCondition(db *gorm.DB, tableName string) {
    if c.Fingerprint != "" {
        db.Where(fmt.Sprintf("%s.fingerprint = ?", tableName), c.Fingerprint)
    }
    if c.Status != "" {
        db.Where(fmt.Sprintf("%s.status = ?", tableName), c.Status)
    }
}

type IDCond struct {
    ID uint
}

func (c *IDCond) BuildCondition(db *gorm.DB, tableName string) {
    db.Where(fmt.Sprintf("%s.id = ?", tableName), c.ID)
}
```

- [ ] **Step 3: Verify `go build ./filestore/` passes**

### Task 3: Refactor store.go to use condition builders

- [ ] **Step 1: Rewrite `GetByID`, `GetByFingerprint`, `UpdateStatus`**

Edit `filestore/store.go`:

- `GetByID`: use `IDCond` with `Model(&FileRecord{})`, remove bare `First`
- `GetByFingerprint`: use `FingerprintCond`
- `UpdateStatus`: use `IDCond`
- `List`: unchanged

- [ ] **Step 2: Verify `go build ./filestore/` passes**

- [ ] **Step 3: Verify `go test ./filestore/ -v` passes**

### Task 4: Add multipart methods to FileStore

- [ ] **Step 1: Add `ErrNotMultipartUpload` to `errors.go`**

- [ ] **Step 2: Add `InitMultipartUploadRequest`/`CompleteMultipartUploadRequest` types + 4 methods to `filestore.go`**

```
InitMultipartUpload(ctx, req):
    CheckExist(fingerprint) → dedup
    NewMultipartUpload(key) → uploader
    Create record with {Status:uploading, UploadID, ChunkSize, StorageKey}
    return record

PresignUploadPartURL(ctx, fileID, partNum, expires):
    GetByID(fileID)
    if UploadID == "" → ErrNotMultipartUpload
    GetMultipartUploader(key, uploadID).PresignUploadPartURL(partNum, expires)

CompleteMultipartUpload(ctx, fileID, parts):
    GetByID(fileID)
    UpdateStatus(id, merging)
    GetMultipartUploader(key, uploadID).Complete(parts)
    UpdateStatus(id, completed)
    clear UploadID field

AbortMultipartUpload(ctx, fileID):
    GetByID(fileID)
    GetMultipartUploader(key, uploadID).Abort()
    UpdateStatus(id, aborted)
    clear UploadID field
```

- [ ] **Step 3: Verify `go build ./filestore/` passes**

### Task 5: Update tests

- [ ] **Step 1: Update mockStorage to implement new interfaces**

- [ ] **Step 2: Add tests for all 4 new methods + condition-based store changes**

- [ ] **Step 3: `go test ./filestore/ -v` — all tests pass**
