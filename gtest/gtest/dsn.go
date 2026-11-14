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
