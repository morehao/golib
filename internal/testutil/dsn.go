package testutil

// 集成测试默认连接串：本机 docker 容器（postgres:18 / mysql:8.4）的约定值。
// 各包测试统一用 GetEnv(<对应环境变量>, <这里>) 取连接串，避免同一字面量散落多处。
// 需要指向别的库时，设置环境变量 MYSQL_DSN / MYSQL_URI / POSTGRES_DSN / POSTGRES_URI 覆盖即可。
//
// DSN 与 URI 是同一实例的两种形态：DSN 给 gorm.Open / 各驱动直接用；
// URI 给 dbgorm 的 Config.URL 用（dbgorm 负责把 mysql:// 归一化成 DSN）。
const (
	// DefaultMySQLDSN 对应环境变量 MySQLDSN。
	DefaultMySQLDSN = "root:123456@tcp(127.0.0.1:3306)/demo?charset=utf8mb4&parseTime=True"
	// DefaultMySQLURI 对应环境变量 MySQLURI，dbgorm 的 Config.URL 形态。
	DefaultMySQLURI = "mysql://root:123456@127.0.0.1:3306/demo?charset=utf8mb4&parseTime=true"
	// DefaultPostgresDSN 对应环境变量 PostgresDSN。
	DefaultPostgresDSN = "host=127.0.0.1 user=postgres password=123456 dbname=demo port=5432 sslmode=disable TimeZone=Asia/Shanghai"
	// DefaultPostgresURI 对应环境变量 PostgresURI，dbgorm 的 Config.URL 形态。
	DefaultPostgresURI = "postgres://postgres:123456@127.0.0.1:5432/demo?sslmode=disable"
)
