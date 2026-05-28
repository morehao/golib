package cos

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/morehao/golib/storage/spec"
	cossdk "github.com/tencentyun/cos-go-sdk-v5"
)

func (c *client) NewMultipartUpload(ctx context.Context, key string, opts ...spec.MultipartOption) (spec.MultipartUploader, error) {
	k, err := spec.NormalizeObjectKey(key)
	if err != nil {
		return nil, err
	}
	mo := spec.ApplyMultipartOptions(opts...)
	initOpt := &cossdk.InitiateMultipartUploadOptions{
		ObjectPutHeaderOptions: &cossdk.ObjectPutHeaderOptions{},
	}
	if mo.ContentType != "" {
		initOpt.ObjectPutHeaderOptions.ContentType = mo.ContentType
	}
	if len(mo.Metadata) > 0 {
		meta := make(http.Header)
		for mk, mv := range mo.Metadata {
			meta.Set(mk, mv)
		}
		initOpt.ObjectPutHeaderOptions.XCosMetaXXX = &meta
	}
	resp, _, err := c.sdk.Object.InitiateMultipartUpload(ctx, k, initOpt)
	if err != nil {
		return nil, fmt.Errorf("storage: create multipart upload %q: %w", k, err)
	}
	return &uploader{
		sdk:      c.sdk,
		key:      k,
		uploadID: resp.UploadID,
	}, nil
}

type uploader struct {
	sdk      *cossdk.Client
	key      string
	uploadID string
}

func (u *uploader) UploadPart(ctx context.Context, partNum int32, reader io.Reader, size int64) (spec.Part, error) {
	if partNum <= 0 {
		return spec.Part{}, fmt.Errorf("storage: part number must be positive, got %d", partNum)
	}
	resp, err := u.sdk.Object.UploadPart(ctx, u.key, u.uploadID, int(partNum), reader, nil)
	if err != nil {
		return spec.Part{}, fmt.Errorf("storage: upload part %d for %q: %w", partNum, u.key, err)
	}
	return spec.Part{
		PartNumber: partNum,
		ETag:       strings.Trim(resp.Header.Get("ETag"), `"`),
	}, nil
}

func (u *uploader) ListParts(ctx context.Context, opts ...spec.ListPartsOption) (*spec.ListPartsResult, error) {
	lo := spec.ApplyListPartsOptions(opts...)
	opt := &cossdk.ObjectListPartsOptions{
		MaxParts: fmt.Sprintf("%d", lo.MaxParts),
	}
	if lo.PartNumberMarker > 0 {
		opt.PartNumberMarker = fmt.Sprintf("%d", lo.PartNumberMarker)
	}
	resp, _, err := u.sdk.Object.ListParts(ctx, u.key, u.uploadID, opt)
	if err != nil {
		return nil, fmt.Errorf("storage: list parts for %q: %w", u.key, err)
	}
	parts := make([]spec.Part, 0, len(resp.Parts))
	for _, p := range resp.Parts {
		parts = append(parts, spec.Part{
			PartNumber:   int32(p.PartNumber),
			ETag:         strings.Trim(p.ETag, `"`),
			Size:         p.Size,
			LastModified: parseTime(p.LastModified),
		})
	}
	nextMarker := int32(0)
	if resp.NextPartNumberMarker != "" {
		marker, _ := strconv.Atoi(resp.NextPartNumberMarker)
		nextMarker = int32(marker)
	}
	return &spec.ListPartsResult{
		Parts:                parts,
		NextPartNumberMarker: nextMarker,
		IsTruncated:          resp.IsTruncated,
	}, nil
}

func (u *uploader) Complete(ctx context.Context, parts []spec.Part) error {
	if len(parts) == 0 {
		return fmt.Errorf("storage: parts list must not be empty")
	}
	for i, p := range parts {
		if p.PartNumber <= 0 {
			return fmt.Errorf("storage: part %d has invalid number %d", i, p.PartNumber)
		}
		if p.ETag == "" {
			return fmt.Errorf("storage: part %d has empty etag", i)
		}
	}
	completedParts := make([]cossdk.Object, 0, len(parts))
	for _, p := range parts {
		completedParts = append(completedParts, cossdk.Object{
			PartNumber: int(p.PartNumber),
			ETag:       p.ETag,
		})
	}
	_, _, err := u.sdk.Object.CompleteMultipartUpload(ctx, u.key, u.uploadID, &cossdk.CompleteMultipartUploadOptions{
		Parts: completedParts,
	})
	if err != nil {
		return fmt.Errorf("storage: complete multipart upload %q: %w", u.key, err)
	}
	return nil
}

func (u *uploader) Abort(ctx context.Context) error {
	_, err := u.sdk.Object.AbortMultipartUpload(ctx, u.key, u.uploadID)
	if err != nil {
		return fmt.Errorf("storage: abort multipart upload %q: %w", u.key, err)
	}
	return nil
}
