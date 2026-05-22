package cos

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	cossdk "github.com/tencentyun/cos-go-sdk-v5"

	"github.com/morehao/golib/storage/internal/core"
	"github.com/morehao/golib/storage/internal/driver"
)

func (c *client) NewMultipartUpload(ctx context.Context, key string, opts driver.MultipartOptions) (driver.MultipartUploader, error) {
	k, err := core.NormalizeObjectKey(key)
	if err != nil {
		return nil, err
	}
	initOpt := &cossdk.InitiateMultipartUploadOptions{
		ObjectPutHeaderOptions: &cossdk.ObjectPutHeaderOptions{},
	}
	if opts.ContentType != "" {
		initOpt.ObjectPutHeaderOptions.ContentType = opts.ContentType
	}
	if len(opts.Metadata) > 0 {
		meta := make(http.Header)
		for mk, mv := range opts.Metadata {
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

func (u *uploader) UploadPart(ctx context.Context, partNum int32, reader io.Reader, size int64) (driver.Part, error) {
	if err := core.ValidatePartNumber(partNum); err != nil {
		return driver.Part{}, err
	}
	resp, err := u.sdk.Object.UploadPart(ctx, u.key, u.uploadID, int(partNum), reader, nil)
	if err != nil {
		return driver.Part{}, fmt.Errorf("storage: upload part %d for %q: %w", partNum, u.key, err)
	}
	return driver.Part{
		PartNumber: partNum,
		ETag:       strings.Trim(resp.Header.Get("ETag"), `"`),
	}, nil
}

func (u *uploader) Complete(ctx context.Context, parts []driver.Part) error {
	cp := make([]core.Part, len(parts))
	for i, p := range parts {
		cp[i] = core.Part{PartNumber: p.PartNumber, ETag: p.ETag}
	}
	if err := core.ValidateParts(cp); err != nil {
		return err
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
