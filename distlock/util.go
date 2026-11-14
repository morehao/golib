package distlock

import "github.com/morehao/golib/gutil"

// GenerateOwner 生成唯一锁持有者标识。
// 作为锁 value 写入 Redis，解锁/续期时用于校验持有者身份，
// 防止误释放他人持有的锁。
//
// 复用 gutil.GenUUID（UUID v7，时间有序），与全仓 id 生成保持一致，
// 避免 distlock 直接依赖 google/uuid。
func GenerateOwner() string {
	return gutil.GenUUID()
}
