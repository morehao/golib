package convert

import (
	"github.com/morehao/golib/storage"
	"github.com/morehao/golib/storage/internal/driver"
)

func ObjectMetaFromDriver(v *driver.ObjectMeta) *storage.ObjectMeta {
	if v == nil {
		return nil
	}
	return &storage.ObjectMeta{
		Key:          v.Key,
		Size:         v.Size,
		ETag:         v.ETag,
		ContentType:  v.ContentType,
		LastModified: v.LastModified,
		Metadata:     v.Metadata,
	}
}

func ListResultFromDriver(v *driver.ListResult) *storage.ListResult {
	if v == nil {
		return nil
	}
	out := &storage.ListResult{
		Objects:   make([]storage.ListedObject, 0, len(v.Objects)),
		NextToken: v.NextToken,
		HasMore:   v.HasMore,
	}
	for _, obj := range v.Objects {
		out.Objects = append(out.Objects, storage.ListedObject{
			Key:          obj.Key,
			Size:         obj.Size,
			ETag:         obj.ETag,
			LastModified: obj.LastModified,
		})
	}
	return out
}

func PartsToDriver(parts []storage.Part) []driver.Part {
	out := make([]driver.Part, 0, len(parts))
	for _, part := range parts {
		out = append(out, driver.Part{PartNumber: part.PartNumber, ETag: part.ETag})
	}
	return out
}
