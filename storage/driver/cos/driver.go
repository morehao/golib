package cos

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/base64"
	"io"
	"net/url"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go/middleware"
	smithyhttp "github.com/aws/smithy-go/transport/http"

	"github.com/morehao/golib/storage"
	"github.com/morehao/golib/storage/driver/s3driver"
)

func init() {
	storage.RegisterStorage(string(storage.DriverCOS), New)
	storage.RegisterPathBuilder(string(storage.DriverCOS), NewPathBuilder)
}

type driver struct {
	*s3driver.Driver
}

var _ storage.Storage = (*driver)(nil)

func NewPathBuilder(cfg storage.Config) storage.PathBuilder {
	return &storage.S3PathBuilder{
		BaseURL:  cfg.BaseURL,
		Endpoint: cfg.Endpoint,
		Region:   cfg.Region,
		URLStyle: storage.URLStyleVirtualHosted,
	}
}

func New(cfg storage.Config) (storage.Storage, error) {
	pb := NewPathBuilder(cfg)

	inner, err := s3driver.New(cfg, pb,
		s3driver.WithS3Options(func(o *s3.Options) {
			o.APIOptions = append(o.APIOptions, func(s *middleware.Stack) error {
				return s.Finalize.Add(cosContentMD5Middleware{}, middleware.Before)
			})
		}),
		s3driver.WithIfNotExistsS3Opt(func(o *s3.Options) {
			o.APIOptions = append(o.APIOptions, smithyhttp.SetHeaderValue("x-cos-forbid-overwrite", "true"))
		}),
	)
	if err != nil {
		return nil, err
	}
	return &driver{Driver: inner.(*s3driver.Driver)}, nil
}

func usePathStyle(endpoint string) bool {
	u, err := url.Parse(endpoint)
	if err != nil {
		return true
	}
	return !strings.Contains(strings.ToLower(u.Hostname()), "myqcloud.com")
}

type cosContentMD5Middleware struct{}

func (m cosContentMD5Middleware) ID() string { return "CosContentMD5" }

func (m cosContentMD5Middleware) HandleFinalize(ctx context.Context, in middleware.FinalizeInput, next middleware.FinalizeHandler) (out middleware.FinalizeOutput, metadata middleware.Metadata, err error) {
	if middleware.GetOperationName(ctx) != "DeleteObjects" {
		return next.HandleFinalize(ctx, in)
	}
	req, ok := in.Request.(*smithyhttp.Request)
	if !ok || req.GetStream() == nil {
		return next.HandleFinalize(ctx, in)
	}
	bodyBytes, readErr := io.ReadAll(req.GetStream())
	if readErr != nil {
		return next.HandleFinalize(ctx, in)
	}
	sum := md5.Sum(bodyBytes)
	req.Header.Set("Content-MD5", base64.StdEncoding.EncodeToString(sum[:]))
	req.SetStream(bytes.NewReader(bodyBytes))
	return next.HandleFinalize(ctx, in)
}

func (m cosContentMD5Middleware) HandleDeserialize(ctx context.Context, in middleware.DeserializeInput, next middleware.DeserializeHandler) (middleware.DeserializeOutput, middleware.Metadata, error) {
	return next.HandleDeserialize(ctx, in)
}
