package testutil

// 集成测试默认连接串：本机 docker 容器（postgres:18 / mysql:8.4）的约定值。
// 各包测试统一用 GetEnv(<对应环境变量>, <这里>) 取连接串，避免同一字面量散落多处。
// 需要指向别的库时，设置环境变量 MYSQL_DSN / POSTGRES_DSN 覆盖即可。
const (
	// DefaultMySQLDSN 对应环境变量 MySQLDSN。
	DefaultMySQLDSN = "root:123456@tcp(127.0.0.1:3306)/demo?charset=utf8mb4&parseTime=True"
	// DefaultPostgresDSN 对应环境变量 PostgresDSN。
	DefaultPostgresDSN = "host=127.0.0.1 user=postgres password=123456 dbname=demo port=5432 sslmode=disable TimeZone=Asia/Shanghai"
)
