package cos

import (
	"fmt"
	"net/http"
	neturl "net/url"

	"github.com/morehao/golib/storage/spec"
	cossdk "github.com/tencentyun/cos-go-sdk-v5"
)

type client struct {
	sdk       *cossdk.Client
	bucket    string
	secretID  string
	secretKey string
}

func New(cfg spec.Config) (spec.Storage, error) {
	u, err := neturl.Parse(cfg.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("storage: parse cos endpoint: %w", err)
	}
	b := &cossdk.BaseURL{BucketURL: u}
	sdk := cossdk.NewClient(b, &http.Client{
		Transport: &cossdk.AuthorizationTransport{
			SecretID:  cfg.AccessKeyID,
			SecretKey: cfg.SecretAccessKey,
		},
	})
	return &client{
		sdk:       sdk,
		bucket:    cfg.Bucket,
		secretID:  cfg.AccessKeyID,
		secretKey: cfg.SecretAccessKey,
	}, nil
}
