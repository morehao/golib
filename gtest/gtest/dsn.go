package gtest

// 集成测试默认连接串：本机 docker 容器（postgres / mysql）的约定值。
// 各包测试统一用 Source.Get(<对应环境变量>, <这里>) 取连接串，避免字面量散落。
// DSN 给 gorm.Open / 各驱动直接用；URI 给 dbgorm 的 Config.URL 用。
const (
	// 环境变量名
	MySQLDSN          = "MYSQL_DSN"
	MySQLURI          = "MYSQL_URI"
	PostgresDSN       = "POSTGRES_DSN"
	PostgresURI       = "POSTGRES_URI"
	RedisAddr         = "REDIS_ADDR"
	RedisPassword     = "REDIS_PASSWORD"
	ElasticsearchAddr = "ELASTICSEARCH_ADDR"

	// 默认连接串（本机 docker 约定）
	DefaultMySQLDSN    = "root:123456@tcp(127.0.0.1:3306)/demo?charset=utf8mb4&parseTime=True"
	DefaultMySQLURI    = "mysql://root:123456@127.0.0.1:3306/demo?charset=utf8mb4&parseTime=true"
	DefaultPostgresDSN = "host=127.0.0.1 user=postgres password=123456 dbname=demo port=5432 sslmode=disable TimeZone=Asia/Shanghai"
	DefaultPostgresURI = "postgres://postgres:123456@127.0.0.1:5432/demo?sslmode=disable"
)

// 加解密测试用密钥的环境变量名（值由 .env 提供）。
const (
	AESKey        = "GOLIB_AES_KEY"
	RSAPrivateKey = "GOLIB_RSA_PRIVATE_KEY"
	RSAPublicKey  = "GOLIB_RSA_PUBLIC_KEY"
)

// 对象存储集成测试环境变量名。
const (
	StorageMinioEndpoint  = "STORAGE_MINIO_ENDPOINT"
	StorageMinioAccessKey = "STORAGE_MINIO_ACCESS_KEY"
	StorageMinioSecretKey = "STORAGE_MINIO_SECRET_KEY"
	StorageMinioUseSSL    = "STORAGE_MINIO_USE_SSL"
	StorageMinioBaseURL   = "STORAGE_MINIO_BASE_URL"
	StorageMinioRegion    = "STORAGE_MINIO_REGION"
	StorageMinioBucket    = "STORAGE_MINIO_BUCKET"

	StorageOSSEndpoint  = "STORAGE_OSS_ENDPOINT"
	StorageOSSAccessKey = "STORAGE_OSS_ACCESS_KEY"
	StorageOSSSecretKey = "STORAGE_OSS_SECRET_KEY"
	StorageOSSBaseURL   = "STORAGE_OSS_BASE_URL"
	StorageOSSRegion    = "STORAGE_OSS_REGION"
	StorageOSSBucket    = "STORAGE_OSS_BUCKET"

	StorageCOSEndpoint  = "STORAGE_COS_ENDPOINT"
	StorageCOSAccessKey = "STORAGE_COS_ACCESS_KEY"
	StorageCOSSecretKey = "STORAGE_COS_SECRET_KEY"
	StorageCOSBaseURL   = "STORAGE_COS_BASE_URL"
	StorageCOSRegion    = "STORAGE_COS_REGION"
	StorageCOSBucket    = "STORAGE_COS_BUCKET"

	StorageTOSEndpoint  = "STORAGE_TOS_ENDPOINT"
	StorageTOSAccessKey = "STORAGE_TOS_ACCESS_KEY"
	StorageTOSSecretKey = "STORAGE_TOS_SECRET_KEY"
	StorageTOSBaseURL   = "STORAGE_TOS_BASE_URL"
	StorageTOSRegion    = "STORAGE_TOS_REGION"
	StorageTOSBucket    = "STORAGE_TOS_BUCKET"

	StorageKodoEndpoint  = "STORAGE_KODO_ENDPOINT"
	StorageKodoAccessKey = "STORAGE_KODO_ACCESS_KEY"
	StorageKodoSecretKey = "STORAGE_KODO_SECRET_KEY"
	StorageKodoBaseURL   = "STORAGE_KODO_BASE_URL"
	StorageKodoRegion    = "STORAGE_KODO_REGION"
	StorageKodoBucket    = "STORAGE_KODO_BUCKET"

	StorageLocalDir    = "STORAGE_LOCAL_DIR"
	StorageLocalBucket = "STORAGE_LOCAL_BUCKET"
)

// gllm 集成测试环境变量名。
const (
	GLLMOpenAIBaseURL = "GLLM_OPENAI_BASE_URL"
	GLLMOpenAIAPIKey  = "GLLM_OPENAI_API_KEY"
	GLLMOpenAIModel   = "GLLM_OPENAI_MODEL"
	GLLMOpenAIRequire = "GLLM_OPENAI_REQUIRE"
)
