package cos

import (
	"fmt"
	"net/http"
	neturl "net/url"

	cossdk "github.com/tencentyun/cos-go-sdk-v5"

	"github.com/morehao/golib/storage"
	"github.com/morehao/golib/storage/spec"
)

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

func init() {
	storage.RegisterProvider(spec.ProviderCOS, New)
}
