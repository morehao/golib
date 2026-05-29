# filestore 预签名有效期可选设计

## 背景

`PresignGetFileURL` 和 `PresignUploadPartURL` 当前要求调用方每次传入 `expires`，缺少实例级默认值。

## 设计

### 1. 包级常量

```go
const defaultPresignExpiry = 24 * time.Hour
```

### 2. `New` 签名

```go
func New(db *gorm.DB, st spec.Storage, opts ...FileStoreOption) (*FileStore, error)
```

未来扩展预留，当前无实际选项。

### 3. PresignOption

```go
type PresignOption func(*presignOptions)

type presignOptions struct {
    expires time.Duration
}

func WithExpires(d time.Duration) PresignOption
```

### 4. 方法签名

```go
func (s *FileStore) PresignGetFileURL(ctx context.Context, id uint, opts ...PresignOption) (string, error)
func (s *FileStore) PresignUploadPartURL(ctx context.Context, id uint, partNum int32, opts ...PresignOption) (string, error)
```

逻辑：
- opts 中提取 expires，若 > 0 则使用
- 否则使用 `defaultPresignExpiry`

## 兼容性

向后兼容，旧调用方不传 opts 即用默认值。

## 未实现

- `FileStoreOption` 选项函数（当前无需，仅预留签名）
