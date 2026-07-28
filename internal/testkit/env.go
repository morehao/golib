package testkit

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/joho/godotenv"
)

// 环境变量名常量
const (
	RedisAddr            = "REDIS_ADDR"
	RedisPassword        = "REDIS_PASSWORD"
	ElasticsearchAddr    = "ELASTICSEARCH_ADDR"
	MySQLDSN             = "MYSQL_DSN"
	MySQLURI             = "MYSQL_URI"
	PostgresDSN          = "POSTGRES_DSN"
	PostgresURI          = "POSTGRES_URI"
	AESKey               = "GOLIB_AES_KEY"
	RSAPrivateKey        = "GOLIB_RSA_PRIVATE_KEY"
	RSAPublicKey         = "GOLIB_RSA_PUBLIC_KEY"
	StorageMinioEndpoint = "STORAGE_MINIO_ENDPOINT"
	StorageMinioAccessKey = "STORAGE_MINIO_ACCESS_KEY"
	StorageMinioSecretKey = "STORAGE_MINIO_SECRET_KEY"
	StorageMinioUseSSL   = "STORAGE_MINIO_USE_SSL"
	StorageMinioBaseURL  = "STORAGE_MINIO_BASE_URL"
	StorageMinioRegion   = "STORAGE_MINIO_REGION"
	StorageOSSEndpoint   = "STORAGE_OSS_ENDPOINT"
	StorageOSSAccessKey  = "STORAGE_OSS_ACCESS_KEY"
	StorageOSSSecretKey  = "STORAGE_OSS_SECRET_KEY"
	StorageOSSBaseURL    = "STORAGE_OSS_BASE_URL"
	StorageOSSRegion     = "STORAGE_OSS_REGION"
	StorageCOSEndpoint   = "STORAGE_COS_ENDPOINT"
	StorageCOSAccessKey  = "STORAGE_COS_ACCESS_KEY"
	StorageCOSSecretKey  = "STORAGE_COS_SECRET_KEY"
	StorageCOSBaseURL    = "STORAGE_COS_BASE_URL"
	StorageCOSRegion     = "STORAGE_COS_REGION"
	StorageTOSEndpoint   = "STORAGE_TOS_ENDPOINT"
	StorageTOSAccessKey  = "STORAGE_TOS_ACCESS_KEY"
	StorageTOSSecretKey  = "STORAGE_TOS_SECRET_KEY"
	StorageTOSBaseURL    = "STORAGE_TOS_BASE_URL"
	StorageTOSRegion     = "STORAGE_TOS_REGION"
	StorageLocalDir      = "STORAGE_LOCAL_DIR"
	StorageTimeout       = "STORAGE_TIMEOUT"
	StorageMaxRetries    = "STORAGE_MAX_RETRIES"
)

func Load() {
	dir, _ := os.Getwd()
	for {
		envPath := filepath.Join(dir, ".env")
		if _, err := os.Stat(envPath); err == nil {
			_ = godotenv.Load(envPath)
			return
		}
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return
		}
		dir = parent
	}
}

// GetEnv 读取环境变量，不存在时返回默认值
func GetEnv(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

// MustEnv 读取环境变量，不存在时调用 t.Fatal
func MustEnv(t testing.TB, key string) string {
	t.Helper()
	v := os.Getenv(key)
	if v == "" {
		t.Fatalf("required environment variable %s is not set", key)
	}
	return v
}
