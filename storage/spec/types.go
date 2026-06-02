package spec

import "time"

type ObjectMeta struct {
	Key          string
	Size         int64
	ETag         string
	ContentType  string
	LastModified time.Time
	Metadata     map[string]string
}

type ListedObject struct {
	Key          string
	Size         int64
	ETag         string
	LastModified time.Time
}

type ListResult struct {
	Objects   []ListedObject
	NextToken string
	HasMore   bool
}

type Part struct {
	PartNumber   int32
	ETag         string
	Size         int64
	LastModified time.Time
}

type UploadInfo struct {
	Key       string
	UploadID  string
	Initiated time.Time
}

type ListMultipartUploadsResult struct {
	Uploads            []UploadInfo
	NextKeyMarker      string
	NextUploadIDMarker string
	IsTruncated        bool
}

type ListPartsResult struct {
	Parts                []Part
	NextPartNumberMarker int32
	IsTruncated          bool
}

type URI struct {
	Provider Provider
	Bucket   string
	Key      string
}
