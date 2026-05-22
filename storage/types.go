package storage

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
	PartNumber int32
	ETag       string
}

type URI struct {
	Provider Provider
	Bucket   string
	Key      string
}
