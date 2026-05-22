package convert

import (
	"github.com/morehao/golib/storage"
	"github.com/morehao/golib/storage/internal/driver"
)

func ConfigToDriver(cfg storage.Config) driver.Config {
	return driver.Config{
		Provider:         driver.Provider(cfg.Provider),
		Endpoint:         cfg.Endpoint,
		Region:           cfg.Region,
		Bucket:           cfg.Bucket,
		AccessKeyID:      cfg.AccessKeyID,
		SecretAccessKey:  cfg.SecretAccessKey,
		SessionToken:     cfg.SessionToken,
		UseSSL:           cfg.UseSSL,
		UsePathStyle:     cfg.UsePathStyle,
		RetryMaxAttempts: cfg.RetryMaxAttempts,
		Timeout:          cfg.Timeout,
		HTTPClient:       cfg.HTTPClient,
	}
}
