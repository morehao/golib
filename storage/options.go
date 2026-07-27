package storage

// PutOption 控制单次上传行为。
type PutOption func(*PutOptions)

// PutOptions 上传选项。
type PutOptions struct {
	ContentType  string            // 对象 Content-Type
	ContentMD5   string            // 服务端内容校验的 MD5（Base64 编码）
	Metadata     map[string]string // 对象自定义元数据
	StorageClass string            // 存储类型（STANDARD / IA / ARCHIVE 等）
	IfNotExists  bool              // true 时仅当 key 不存在才写入
}

// WithContentType 设置对象的 Content-Type。
func WithContentType(ct string) PutOption {
	return func(o *PutOptions) { o.ContentType = ct }
}

// WithContentMD5 设置服务端内容校验的 MD5（Base64 编码）。
func WithContentMD5(md5 string) PutOption {
	return func(o *PutOptions) { o.ContentMD5 = md5 }
}

// WithMetadata 设置对象自定义元数据。
func WithMetadata(m map[string]string) PutOption {
	return func(o *PutOptions) { o.Metadata = m }
}

// WithStorageClass 设置存储类型（STANDARD / IA / ARCHIVE 等）。
func WithStorageClass(sc string) PutOption {
	return func(o *PutOptions) { o.StorageClass = sc }
}

// WithIfNotExists 仅当 key 不存在时才写入，否则返回 ErrAlreadyExists。
func WithIfNotExists() PutOption {
	return func(o *PutOptions) { o.IfNotExists = true }
}

// GetOption 控制下载行为。
type GetOption func(*GetOptions)

// GetOptions 下载选项。
type GetOptions struct {
	ByteRange *ByteRange // 字节范围，nil 表示下载整个对象
}

// ByteRange 字节范围，闭区间 [Start, End]。
type ByteRange struct {
	Start int64 // 起始字节偏移（包含）
	End   int64 // 结束字节偏移（包含）
}

// WithByteRange 限定下载字节范围。
func WithByteRange(start, end int64) GetOption {
	return func(o *GetOptions) { o.ByteRange = &ByteRange{Start: start, End: end} }
}

// ListOption 控制列举行为。
type ListOption func(*ListOptions)

type ListOptions struct {
	MaxKeys           int64  // 单次返回的最大 key 数，0 表示不限制
	StartAfter        string // 按 key 顺序从该 key 之后开始列举
	ContinuationToken string // 服务端分页游标，配合 NextContinuationToken 使用；与 StartAfter 互斥
	Recursive         bool   // true 时递归列举所有层级的 key
}

// WithMaxKeys 限制单次返回的最大 key 数。
func WithMaxKeys(n int64) ListOption {
	return func(o *ListOptions) { o.MaxKeys = n }
}

// WithStartAfter 从指定 key 之后开始列举。
func WithStartAfter(k string) ListOption {
	return func(o *ListOptions) { o.StartAfter = k }
}

// WithContinuationToken 使用服务端分页游标继续之前的列举。
func WithContinuationToken(t string) ListOption {
	return func(o *ListOptions) { o.ContinuationToken = t }
}

// WithRecursive 设置为 true 时递归列举所有 key（忽略 delimiter）。
func WithRecursive(r bool) ListOption {
	return func(o *ListOptions) { o.Recursive = r }
}
