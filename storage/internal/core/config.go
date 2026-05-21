package core

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
	S3       *S3Config
	MinIO    *MinIOConfig
	OSS      *OSSConfig
	COS      *COSConfig
	TOS      *TOSConfig
}

type S3Config struct {
	Endpoint  string
	Region    string
	AccessKey string
	SecretKey string
	Bucket    string
	UseSSL    bool
}

type MinIOConfig struct {
	Endpoint  string
	AccessKey string
	SecretKey string
	Bucket    string
	UseSSL    bool
}

type OSSConfig struct {
	Endpoint  string
	Region    string
	AccessKey string
	SecretKey string
	Bucket    string
}

type COSConfig struct {
	Endpoint  string
	Region    string
	SecretID  string
	SecretKey string
	Bucket    string
}

type TOSConfig struct {
	Endpoint  string
	Region    string
	AccessKey string
	SecretKey string
	Bucket    string
}
