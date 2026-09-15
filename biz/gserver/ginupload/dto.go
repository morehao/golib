package ginupload

import "net/http"

// --- uri ---

// fileIDURI 绑定文件资源路径参数 :id（gin 原生 ShouldBindUri）
type fileIDURI struct {
	ID string `uri:"id" binding:"required"` // 文件ID
}

// multipartFileIDURI 绑定 multipart 子资源路径参数 :fileID（gin 原生 ShouldBindUri）
type multipartFileIDURI struct {
	FileID string `uri:"fileID" binding:"required"` // 文件ID
}

// --- common ---

// presignURLResponse 预签名请求的对外契约：客户端必须按 Method + URL + Headers
// 原样发起请求。Headers 是签名覆盖的头（Content-Type / Content-MD5 / X-Amz-Meta-* 等），
// 漏发任意一个都会得到 SignatureDoesNotMatch，因此这里必须透传而不是只给 URL。
type presignURLResponse struct {
	URL       string      `json:"url"`        // 预签名URL
	Method    string      `json:"method"`     // 预签名请求的 HTTP 方法
	Headers   http.Header `json:"headers"`    // 签名覆盖的请求头，必须原样发送
	ExpiresIn int         `json:"expires_in"` // 过期时间(秒)
}

type uploadPart struct {
	PartNumber int32  `json:"part_number" binding:"required,gt=0"` // 分片编号
	ETag       string `json:"etag"`                                // 分片ETag
}

type fileRecordResponse struct {
	FileID   string `json:"file_id"`   // 文件ID
	Name     string `json:"name"`      // 文件名
	MimeType string `json:"mime_type"` // MIME类型
	Status   string `json:"status"`    // 文件状态(pending/uploading/completed/failed/aborted)
}

// --- upload ---

// uploadRequest 描述 /files 的 multipart 表单字段契约。
// handler 采用流式解析（multipart.Reader）而非 ShouldBind，此结构体仅作文档用途，
// 与 swagger 注解保持一致。
type uploadRequest struct {
	ContentHash string `form:"content_hash" binding:"required"` // 内容哈希
}

type checkExistRequest struct {
	ContentHash string `json:"content_hash" form:"content_hash" binding:"required"` // 内容哈希
}

type checkExistResponse struct {
	Exists bool                `json:"exists"`         // 是否存在
	File   *fileRecordResponse `json:"file,omitempty"` // 文件记录(存在时返回)
}

type createMultipartRequest struct {
	ContentHash string `json:"content_hash" binding:"required"` // 内容哈希
	Name        string `json:"name" binding:"required"`         // 文件名
	Size        int64  `json:"size" binding:"required"`         // 文件大小(字节)
	MimeType    string `json:"mime_type"`                       // MIME类型
}

type createMultipartResponse struct {
	FileID   string `json:"file_id"`   // 文件ID
	UploadID string `json:"upload_id"` // 上传会话ID
}

// presignPartRequest 混合绑定：FileID 来自路径参数 :fileID（gincontext.BindPathParams），
// PartNumber 来自 JSON body（ShouldBindJSON），最终由 validator 统一校验。
// json:"-" 保证 FileID 无法被 body 携带/覆盖，路径参数是唯一来源。
type presignPartRequest struct {
	FileID     string `uri:"fileID" json:"-" binding:"required"`                      // 文件ID（路径参数）
	PartNumber int32  `json:"part_number" form:"part_number" binding:"required,gt=0"` // 分片编号
}

// completeMultipartRequest 混合绑定：FileID 来自路径参数 :fileID，Parts 来自 JSON body。
type completeMultipartRequest struct {
	FileID string       `uri:"fileID" json:"-" binding:"required"` // 文件ID（路径参数）
	Parts  []uploadPart `json:"parts"`                             // 分片列表
}

// --- file ---

type getFileQueryRequest struct {
	FileID     string `form:"file_id"`     // 文件ID
	StorageURI string `form:"storage_uri"` // 存储URI
}

type fileDetailResponse struct {
	FileID      string `json:"file_id"`             // 文件ID
	ContentHash string `json:"content_hash"`        // 内容哈希
	Name        string `json:"name"`                // 文件名
	Size        int64  `json:"size"`                // 文件大小(字节)
	MimeType    string `json:"mime_type"`           // MIME类型
	StorageURI  string `json:"storage_uri"`         // 存储URI
	UploadID    string `json:"upload_id,omitempty"` // 上传会话ID(仅分片上传时有值)
	Status      string `json:"status"`              // 文件状态
	CreatedAt   string `json:"created_at"`          // 创建时间(RFC3339)
	UpdatedAt   string `json:"updated_at"`          // 更新时间(RFC3339)
}

// --- presign ---

type presignedPutResponse struct {
	URI string `json:"uri"` // 存储 URI
}

type presignedPartResponse struct {
	PartNumber int    `json:"part_number"` // 分片编号
	ETag       string `json:"etag"`        // 分片 ETag（内容 MD5），complete 时回传
}

// listPartsResponse 分片会话已上传分片列表。IsTruncated 为 true 表示后端单页上限
// （S3 为 1000 片）截断，客户端需用 NextPartNumberMarker 继续列举。
type listPartsResponse struct {
	Parts                []presignedPartResponse `json:"parts"`                             // 已上传分片
	IsTruncated          bool                    `json:"is_truncated"`                      // 是否被截断
	NextPartNumberMarker int32                   `json:"next_part_number_marker,omitempty"` // 下一页游标
}

// listPartsQueryRequest 分页参数，均为可选；缺省时由后端返回首页。
type listPartsQueryRequest struct {
	MaxParts         int32 `form:"max_parts" binding:"omitempty,gt=0"`           // 单页最大分片数
	PartNumberMarker int32 `form:"part_number_marker" binding:"omitempty,gte=0"` // 从该分片号之后继续
}
