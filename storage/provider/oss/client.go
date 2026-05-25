package oss

import (
	aliyun "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss/credentials"
	"github.com/morehao/golib/storage/spec"
)

type client struct {
	sdk    *aliyun.Client
	bucket string
}

func New(cfg spec.Config) (spec.Storage, error) {
	cred := credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, cfg.SessionToken)
	c := aliyun.NewClient(
		aliyun.NewConfig().
			WithRegion(cfg.Region).
			WithEndpoint(cfg.Endpoint).
			WithCredentialsProvider(cred),
	)
	return &client{sdk: c, bucket: cfg.Bucket}, nil
}
