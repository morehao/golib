package ginupload

// --- common ---

type fileIDRequest struct {
	FileID uint `json:"file_id" form:"file_id" binding:"required"` // 文件ID
}

type presignURLResponse struct {
	URL       string `json:"url"`        // 预签名URL
	ExpiresIn int    `json:"expires_in"` // 过期时间(秒)
}

type uploadPart struct {
	PartNumber int32  `json:"part_number"` // 分片编号
	ETag       string `json:"etag"`        // 分片ETag
}

type fileRecordResponse struct {
	FileID      uint   `json:"file_id"`      // 文件ID
	Fingerprint string `json:"fingerprint"`  // 文件指纹(SHA256)
	Name        string `json:"name"`         // 文件名
	Size        int64  `json:"size"`         // 文件大小(字节)
	MimeType    string `json:"mime_type"`    // MIME类型
	StoragePath string `json:"storage_path"` // 存储路径
	Status      string `json:"status"`       // 状态: uploading/completed/aborted
}

// --- upload ---

type checkExistRequest struct {
	Fingerprint string `json:"fingerprint" form:"fingerprint" binding:"required"` // 文件指纹
}

type checkExistResponse struct {
	Exists bool                `json:"exists"`           // 是否存在
	File   *fileRecordResponse `json:"file,omitempty"`   // 文件记录(存在时返回)
}

type createMultipartRequest struct {
	Fingerprint string `json:"fingerprint" binding:"required"` // 文件指纹
	Name        string `json:"name" binding:"required"`        // 文件名
	Size        int64  `json:"size" binding:"required"`        // 文件大小(字节)
	MimeType    string `json:"mime_type"`                      // MIME类型
	StoragePath string `json:"storage_path"`                   // 存储路径
}

type createMultipartResponse struct {
	FileID      uint   `json:"file_id"`      // 文件ID
	UploadID    string `json:"upload_id"`    // 分片上传ID(S3 UploadID)
	Fingerprint string `json:"fingerprint"`  // 文件指纹
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
	FileID      uint   `json:"file_id"`               // 文件ID
	Fingerprint string `json:"fingerprint"`            // 文件指纹(SHA256)
	Name        string `json:"name"`                   // 文件名
	Size        int64  `json:"size"`                   // 文件大小(字节)
	MimeType    string `json:"mime_type"`               // MIME类型
	StoragePath string `json:"storage_path"`            // 存储路径
	UploadID    string `json:"upload_id,omitempty"`     // 分片上传ID
	Status      string `json:"status"`                  // 状态: uploading/completed/aborted
	CreatedAt   string `json:"created_at"`              // 创建时间
	UpdatedAt   string `json:"updated_at"`              // 更新时间
}

type presignDownloadRequest struct {
	FileID uint `json:"file_id" form:"file_id" binding:"required"` // 文件ID
}
