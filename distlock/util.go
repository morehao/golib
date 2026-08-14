package distlock

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// GenerateOwner 生成唯一锁持有者标识（时间戳 + UUID）。
// 作为锁 value 写入 Redis，解锁/续期时用于校验持有者身份，
// 防止误释放他人持有的锁。
func GenerateOwner() string {
	return fmt.Sprintf("%d-%s", time.Now().UnixNano(), uuid.New().String())
}
