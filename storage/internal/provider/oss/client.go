package oss

import (
	aliyun "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss/credentials"

	"github.com/morehao/golib/storage"
)

type client struct {
	sdk    *aliyun.Client
	bucket string
}

func init() {
	storage.RegisterProvider(storage.ProviderOSS, New)
}

func New(cfg storage.Config) (storage.Storage, error) {
	cred := credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, cfg.SessionToken)
	c := aliyun.NewClient(
		aliyun.NewConfig().
			WithRegion(cfg.Region).
			WithEndpoint(cfg.Endpoint).
			WithCredentialsProvider(cred),
	)
	return &client{sdk: c, bucket: cfg.Bucket}, nil
}
