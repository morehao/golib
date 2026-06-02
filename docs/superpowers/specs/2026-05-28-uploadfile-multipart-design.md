# filestore multipart upload support

## Summary

Extend `filestore` to support S3 multipart upload with client-direct-to-S3 strategy. No local chunk table needed — S3 manages all part lifecycle. Clients upload parts via presigned URLs and notify the server on completion.

## Model changes

### FileRecord new fields

```go
UploadID  string `gorm:"column:upload_id;type:varchar(128);comment:S3 multipart upload session ID"`
ChunkSize int64  `gorm:"column:chunk_size;comment:standard chunk size in bytes (0 for non-multipart)"`
```

### New status

```go
FileStatusMerging FileStatus = "merging" // parts uploaded, completing
```

### Index

Add index on `upload_id` for lookup during Complete / Abort / Presign operations.

## Interface changes

### spec.Storage — MultipartUploader

Add presigned URL support to the existing `MultipartUploader`:

```go
type MultipartUploader interface {
    UploadPart(ctx, partNum int32, reader io.Reader, size int64) (Part, error)
    PresignUploadPartURL(ctx, partNum int32, expires time.Duration) (string, error) // NEW
    Complete(ctx, parts []Part) error
    Abort(ctx) error
    ListParts(ctx, opts ...ListPartsOption) (*ListPartsResult, error)
}
```

### spec.Storage — new method

`NewMultipartUpload` creates a new upload. `GetMultipartUploader` returns a handle for an **existing** upload — required for Complete / Abort / Presign after the initial creation:

```go
type Storage interface {
    // ... existing methods ...
    NewMultipartUpload(ctx, key string, opts ...MultipartOption) (MultipartUploader, error)
    GetMultipartUploader(ctx, key, uploadID string) (MultipartUploader, error) // NEW
    // ...
}
```

## FileStore new methods

### InitMultipartUpload

```
Input:  Fingerprint, Name, Size, MimeType, ChunkSize, StorageKey, StorageURI
Flow:   CheckExist(fingerprint) → dedup
        st.NewMultipartUpload(storageKey) → uploader
        db.Create(FileRecord{Status: uploading, UploadID, ChunkSize})
        return record
```

### PresignUploadPartURL

```
Input:  fileID, partNum, expires
Flow:   store.GetByID(fileID)
        if record.UploadID == "" → error (not a multipart upload)
        uploader = st.GetMultipartUploader(record.StorageKey, record.UploadID)
        url = uploader.PresignUploadPartURL(partNum, expires)
        return url
```

### CompleteMultipartUpload

```
Input:  fileID, parts []Part {PartNumber, ETag}
Flow:   store.GetByID(fileID)
        uploader = st.GetMultipartUploader(record.StorageKey, record.UploadID)
        store.UpdateStatus(id, merging)
        uploader.Complete(parts)
        store.UpdateStatus(id, completed)
        clear UploadID field (upload session is done)
```

### AbortMultipartUpload

```
Input:  fileID
Flow:   store.GetByID(fileID)
        uploader = st.GetMultipartUploader(record.StorageKey, record.UploadID)
        uploader.Abort()
        store.UpdateStatus(id, aborted)
        clear UploadID field
```

## Usage flow

```
1. Client calculates SHA256 of the entire file.
2. Client calls CheckExist(fingerprint) — hit → done (dedup).
3. Client calls InitMultipartUpload(fingerprint, name, size, chunkSize, storageURI).
   Server creates S3 multipart upload, persists FileRecord, returns record.
4. For chunk N:
   a. Client calls PresignUploadPartURL(fileID, N, expires).
   b. Client PUTs chunk data directly to S3 via the presigned URL.
   c. Client saves the returned ETag.
5. After all chunks uploaded, client calls CompleteMultipartUpload(fileID, parts).
   Server calls S3 CompleteMultipartUpload, updates status to completed.
6. (On failure) Client calls AbortMultipartUpload(fileID).
   Server calls S3 AbortMultipartUpload, updates status to aborted.
```

## Error handling

- **Dedup race**: `Fingerprint` unique index prevents duplicate records. If `InitMultipartUpload` hits a duplicate key, fall back to `GetByFingerprint` with `completed` status.
- **Stale uploads**: S3 has automatic lifecycle policies for incomplete multipart uploads. Server-side `ListParts` can be exposed if needed for client recovery.
- **Complete failure**: If `Complete` fails with a network error, the uploader can retry with the same parts list (S3 is idempotent for CompleteMultipartUpload).

## Testing

- Add mock methods for `PresignUploadPartURL` and `GetMultipartUploader` to `mockStorage`.
- Test `InitMultipartUpload` — success, dedup hit, dedup race (TOCTOU).
- Test `PresignUploadPartURL` — success, file not found, not a multipart upload.
- Test `CompleteMultipartUpload` — success, status transition (uploading→merging→completed).
- Test `AbortMultipartUpload` — success, status transition (uploading→aborted).
- Test non-multipart operations (RecordUpload, UploadAndRecord) remain unaffected.

## Store refactor

### Goal

Eliminate raw `Where("col = ?", val)` in `store.go`. All query conditions use `model.go` condition types.

### New conditions in model.go

```go
type FingerprintCond struct {
    Fingerprint string
    Status      FileStatus
}
func (c *FingerprintCond) BuildCondition(db *gorm.DB, tableName string)

type IDCond struct {
    ID uint
}
func (c *IDCond) BuildCondition(db *gorm.DB, tableName string)
```

### store.go changes

| Method | Current | New |
|--------|---------|-----|
| `Create` | `db.Create` | unchanged |
| `GetByID` | `First(&rec, id)` | `Model().Scopes(IDCond{...})` |
| `GetByFingerprint` | `Where("fingerprint = ? AND status = ?")` | `Model().Scopes(FingerprintCond{...})` |
| `UpdateStatus` | `Model().Where("id = ?").Update(...)` | `Model().Scopes(IDCond{...}).Update(...)` |
| `List` | Already uses fileCond | unchanged |
| `Delete` | `Delete(&FileRecord{}, id)` | unchanged |
