package ginupload

// --- uri ---

// fileIDURI 绑定文件资源路径参数 :id（gin 原生 ShouldBindUri）
type fileIDURI struct {
	ID uint `uri:"id" binding:"required,gt=0"` // 文件ID
}

// multipartFileIDURI 绑定 multipart 子资源路径参数 :fileID（gin 原生 ShouldBindUri）
type multipartFileIDURI struct {
	FileID uint `uri:"fileID" binding:"required,gt=0"` // 文件ID
}

// --- common ---

type presignURLResponse struct {
	URL       string `json:"url"`        // 预签名URL
	ExpiresIn int    `json:"expires_in"` // 过期时间(秒)
}

type uploadPart struct {
	PartNumber int32  `json:"part_number" binding:"required,gt=0"` // 分片编号
	ETag       string `json:"etag"`                                // 分片ETag
}

type fileRecordResponse struct {
	FileID   uint   `json:"file_id"`   // 文件ID
	Name     string `json:"name"`      // 文件名
	MimeType string `json:"mime_type"` // MIME类型
	Status   string `json:"status"`    // 文件状态(pending/uploading/completed/failed/aborted)
}

// --- upload ---

type uploadRequest struct {
	ContentHash string `form:"content_hash" binding:"required"` // 内容哈希
}

type checkExistRequest struct {
	ContentHash string `json:"content_hash" form:"content_hash" binding:"required"` // 内容哈希
}

type checkExistResponse struct {
	Exists bool                `json:"exists"`           // 是否存在
	File   *fileRecordResponse `json:"file,omitempty"`   // 文件记录(存在时返回)
}

type createMultipartRequest struct {
	ContentHash string `json:"content_hash" binding:"required"` // 内容哈希
	Name        string `json:"name" binding:"required"`         // 文件名
	Size        int64  `json:"size" binding:"required"`         // 文件大小(字节)
	MimeType    string `json:"mime_type"`                       // MIME类型
	StoragePath string `json:"storage_path"`                    // 存储路径
}

type createMultipartResponse struct {
	FileID   uint   `json:"file_id"`   // 文件ID
	UploadID string `json:"upload_id"` // 上传会话ID
}

type presignPartRequest struct {
	PartNumber int32 `json:"part_number" form:"part_number" binding:"required,gt=0"` // 分片编号
}

type completeMultipartRequest struct {
	Parts []uploadPart `json:"parts"` // 分片列表
}

// --- file ---

type getFileQueryRequest struct {
	FileID     uint   `form:"file_id"`     // 文件ID
	StorageURI string `form:"storage_uri"` // 存储URI
}

type fileDetailResponse struct {
	FileID      uint   `json:"file_id"`                 // 文件ID
	ContentHash string `json:"content_hash"`            // 内容哈希
	Name        string `json:"name"`                    // 文件名
	Size        int64  `json:"size"`                    // 文件大小(字节)
	MimeType    string `json:"mime_type"`               // MIME类型
	StorageURI  string `json:"storage_uri"`             // 存储URI
	UploadID    string `json:"upload_id,omitempty"`     // 上传会话ID(仅分片上传时有值)
	Status      string `json:"status"`                  // 文件状态
	CreatedAt   string `json:"created_at"`              // 创建时间(RFC3339)
	UpdatedAt   string `json:"updated_at"`              // 更新时间(RFC3339)
}

// --- presign ---

type presignedPutResponse struct {
	URI string `json:"uri"` // 存储 URI
}
