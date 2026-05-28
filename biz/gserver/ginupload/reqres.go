package ginupload

// --- common ---

type fileIDRequest struct {
	ID uint `json:"id" form:"id"`
}

type presignURLResponse struct {
	URL       string `json:"url"`
	ExpiresIn int    `json:"expires_in"`
}

type uploadPart struct {
	PartNumber int32  `json:"part_number"`
	ETag       string `json:"etag"`
}

type fileRecordResponse struct {
	ID          uint   `json:"id"`
	Fingerprint string `json:"fingerprint"`
	Name        string `json:"name"`
	Size        int64  `json:"size"`
	MimeType    string `json:"mime_type"`
	StoragePath string `json:"storage_path"`
	Status      string `json:"status"`
}

// --- upload ---

type checkExistRequest struct {
	Fingerprint string `json:"fingerprint" form:"fingerprint"`
}

type checkExistResponse struct {
	Exists bool                `json:"exists"`
	File   *fileRecordResponse `json:"file,omitempty"`
}

type initMultipartRequest struct {
	Fingerprint string `json:"fingerprint"`
	Name        string `json:"name"`
	Size        int64  `json:"size"`
	MimeType    string `json:"mime_type"`
	StoragePath string `json:"storage_path"`
}

type initMultipartResponse struct {
	ID          uint   `json:"id"`
	UploadID    string `json:"upload_id"`
	Fingerprint string `json:"fingerprint"`
}

type presignPartRequest struct {
	ID         uint   `json:"id" form:"id"`
	PartNumber int32  `json:"part_number" form:"part_number"`
	Expires    string `json:"expires" form:"expires"`
}

type completeMultipartRequest struct {
	ID    uint         `json:"id"`
	Parts []uploadPart `json:"parts"`
}

// --- file ---

type fileDetailResponse struct {
	ID          uint   `json:"id"`
	Fingerprint string `json:"fingerprint"`
	Name        string `json:"name"`
	Size        int64  `json:"size"`
	MimeType    string `json:"mime_type"`
	StoragePath string `json:"storage_path"`
	UploadID    string `json:"upload_id,omitempty"`
	Status      string `json:"status"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

type presignDownloadRequest struct {
	ID      uint   `json:"id" form:"id"`
	Expires string `json:"expires" form:"expires"`
}
