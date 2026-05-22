package driver

import (
	"net/http"
	"time"
)

type Provider string

const (
	ProviderS3    Provider = "s3"
	ProviderMinIO Provider = "minio"
	ProviderOSS   Provider = "oss"
	ProviderCOS   Provider = "cos"
	ProviderTOS   Provider = "tos"
)

type Config struct {
	Provider Provider
	Endpoint string
	Region   string
	Bucket   string

	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string

	UseSSL           bool
	UsePathStyle     bool
	RetryMaxAttempts int
	Timeout          time.Duration
	HTTPClient       *http.Client
}
