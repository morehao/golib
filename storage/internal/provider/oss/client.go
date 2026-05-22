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

)
	return &client{sdk: c, bucket: cfg.Bucket}, nil
}
