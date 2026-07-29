package ginupload

// --- common ---

type fileIDRequest struct {
	FileID uint `json:"file_id" form:"file_id" binding:"required"` // 文件ID(core_file_upload.id)
}

type presignURLResponse struct {
	URL       string `json:"url"`        // 预签名URL
	ExpiresIn int    `json:"expires_in"` // 过期时间(秒)
}

type uploadPart struct {
	PartNumber int32  `json:"part_number" binding:"required,gt=0"` // 分片编号
	ETag       string `json:"etag"`                                // 分片ETag
}

type fileRecordResponse struct {
	FileID   uint   `json:"file_id"`
	Name     string `json:"name"`
	MimeType string `json:"mime_type"`
	Status   string `json:"status"`
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
	Name        string `json:"name" binding:"required"`
	Size        int64  `json:"size" binding:"required"`
	MimeType    string `json:"mime_type"`
	StoragePath string `json:"storage_path"`
}

type createMultipartResponse struct {
	FileID   uint   `json:"file_id"`
	UploadID string `json:"upload_id"`
}

type presignPartRequest struct {
	FileID     uint  `json:"file_id" form:"file_id" binding:"required"`              // 文件ID
	PartNumber int32 `json:"part_number" form:"part_number" binding:"required,gt=0"` // 分片编号
}

type completeMultipartRequest struct {
	FileID uint         `json:"file_id" binding:"required"` // 文件ID
	Parts  []uploadPart `json:"parts"`                      // 分片列表
}

// --- file ---

type fileDetailResponse struct {
	FileID      uint   `json:"file_id"`
	ContentHash string `json:"content_hash"` // 内容哈希
	Name        string `json:"name"`
	Size        int64  `json:"size"`
	MimeType    string `json:"mime_type"`
	StorageURI  string `json:"storage_uri"`
	UploadID    string `json:"upload_id,omitempty"`
	Status      string `json:"status"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

type presignDownloadRequest struct {
	FileID uint `json:"file_id" form:"file_id" binding:"required"` // 文件ID
}

// --- presign ---

type presignedPutResponse struct {
	URI string `json:"uri"` // 存储 URI
}
