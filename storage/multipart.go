package storage

// CreateMultipartInput 创建分片上传会话的入参。
//
// 用结构体而不是 ...PutOption：变参 option 允许实现"解析了却不用"，
// 那正是分片上传静默丢弃 Metadata/StorageClass 的成因。
// 结构体 + 统一转换（FromPutOptions）强制实现穷尽字段。
type CreateMultipartInput struct {
	ContentType  string
	Metadata     map[string]string
	StorageClass string
	// Size 是调用方在创建会话时声明的整对象大小，0 表示不声明。
	// 驱动不消费它做校验（客户端直传路径下服务端看不到字节），
	// 由上层在 CompleteMultipart 返回后用 ObjectInfo.Size 与之对账。
	Size int64
}

// FromPutOptions 把上传选项转换为会话入参，供需要复用同一套选项的调用方使用。
func FromPutOptions(opts ...PutOption) CreateMultipartInput {
	var o PutOptions
	for _, fn := range opts {
		if fn != nil {
			fn(&o)
		}
	}
	return CreateMultipartInput{
		ContentType:  o.ContentType,
		Metadata:     o.Metadata,
		StorageClass: o.StorageClass,
	}
}

// ListPartsOption 控制 ListParts 行为。
type ListPartsOption func(*ListPartsOptions)

// ListPartsOptions ListParts 选项。
type ListPartsOptions struct {
	MaxParts         int32 // 单页最大分片数，0 表示用后端默认
	PartNumberMarker int32 // 从该分片号之后继续列举
}

// WithMaxParts 限制单页返回的最大分片数。
func WithMaxParts(n int32) ListPartsOption {
	return func(o *ListPartsOptions) { o.MaxParts = n }
}

// WithPartNumberMarker 从指定分片号之后继续列举。
func WithPartNumberMarker(n int32) ListPartsOption {
	return func(o *ListPartsOptions) { o.PartNumberMarker = n }
}

// ListPartsOutput ListParts 单次调用结果。
//
// 不用 []PartInfo 是因为 S3 的 ListParts 单页上限是 1000，而一次分片上传
// 最多 10000 片：只返回切片会**静默截断**，调用方无法察觉还有后 9000 片。
// 这正是本次重构要消灭的"抽象比协议窄"。
type ListPartsOutput struct {
	Parts                []PartInfo
	IsTruncated          bool
	NextPartNumberMarker int32 // 下一页游标，配合 WithPartNumberMarker 使用
}

// ValidateParts 校验分片列表是否满足后端声明的协议约束，所有驱动共用。
//
// 返回的错误包装 ErrInvalidArgument，调用方可直接向上透传。
//
// 校验项（仅在后端对应上限 > 0 时生效，0 表示无限制）：
//   - 分片号 ∈ [1, MaxParts]；
//   - 分片号严格升序且不重复（S3 的 CompleteMultipartUpload 要求请求内
//     分片按升序排列，乱序会被后端拒绝，此处提前给出明确错误）；
//   - 片数 <= MaxParts；
//   - Size > 0 的分片不超过 MaxSinglePut（单分片上限与单次 PUT 上限同为 5 GiB）；
//   - 除末片外，Size > 0 的分片不小于 MinPartSize。
//
// **已知的不可提前校验项**：非末片小于 MinPartSize 无法在 UploadPart 阶段
// 判定 —— 上传第 N 片时第 N+1 片可能尚未产生，"这片是不是末片"是未知的。
// S3 自身也只能在 complete 时报 EntityTooSmall。因此该情形仍以后端返回为准，
// 由错误分类映射为 ErrEntityTooSmall；Size 为 0（客户端只回传 ETag）的分片
// 跳过大小校验。
func ValidateParts(parts []PartInfo, caps Caps) error {
	maxParts := caps.Limits.MaxParts
	if maxParts > 0 && len(parts) > maxParts {
		return wrapInvalidArgument("too many parts: %d > %d", len(parts), maxParts)
	}
	for i, p := range parts {
		if p.PartNumber < 1 {
			return wrapInvalidArgument("part number must be >= 1, got %d", p.PartNumber)
		}
		if maxParts > 0 && int(p.PartNumber) > maxParts {
			return wrapInvalidArgument("part number %d exceeds max parts %d", p.PartNumber, maxParts)
		}
		if i > 0 && p.PartNumber <= parts[i-1].PartNumber {
			return wrapInvalidArgument(
				"parts must be in strictly ascending order: part %d follows part %d",
				p.PartNumber, parts[i-1].PartNumber)
		}
		if p.Size < 0 {
			return wrapInvalidArgument("part %d size must not be negative, got %d", p.PartNumber, p.Size)
		}
		if p.Size == 0 {
			continue // 大小未知，跳过大小相关校验
		}
		if caps.Limits.MaxSinglePut > 0 && p.Size > caps.Limits.MaxSinglePut {
			return wrapInvalidArgument("part %d size %d exceeds limit %d",
				p.PartNumber, p.Size, caps.Limits.MaxSinglePut)
		}
		isLast := i == len(parts)-1
		if !isLast && caps.Limits.MinPartSize > 0 && p.Size < caps.Limits.MinPartSize {
			return wrapInvalidArgument("part %d size %d is below min part size %d",
				p.PartNumber, p.Size, caps.Limits.MinPartSize)
		}
	}
	return nil
}

// ValidatePartCount 在 UploadPart 阶段做"单个分片"可判定的校验。
// 大小相关的约束留到 CompleteMultipart（见 ValidateParts 的说明）。
func ValidatePartCount(number int32, caps Caps) error {
	if number < 1 {
		return wrapInvalidArgument("part number must be >= 1, got %d", number)
	}
	if caps.Limits.MaxParts > 0 && int(number) > caps.Limits.MaxParts {
		return wrapInvalidArgument("part number %d exceeds max parts %d", number, caps.Limits.MaxParts)
	}
	return nil
}
