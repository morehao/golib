package cos

import (
	"fmt"
	"net/http"
	neturl "net/url"

	cossdk "github.com/tencentyun/cos-go-sdk-v5"

	"github.com/morehao/golib/storage/internal/core"
)

type client struct {
	sdk       *cossdk.Client
	bucket    string
	secretID  string
	secretKey string
}

func New(cfg core.Config) (core.Storage, error) {
	u, err := neturl.Parse(cfg.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("storage: parse cos endpoint: %w", err)
	}
	b := &cossdk.BaseURL{BucketURL: u}
	sdk := cossdk.NewClient(b, &http.Client{
		Transport: &cossdk.AuthorizationTransport{
			// COS SDK uses SecretID/SecretKey terminology.
			// The flattened Config.AccessKeyID maps to COS SecretID,
			// and Config.SecretAccessKey maps to COS SecretKey.
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
