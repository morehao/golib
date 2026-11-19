package manager

import (
	"context"

	"github.com/morehao/golib/task/gasync"
)

// resolveScope 解析当前请求的作用域取值并给出是否强制（口径与 source/db 完全一致）。
//
//   - resolver == nil 且 RequireScope == false：灰度兼容，不强制；
//   - resolver == nil 且 RequireScope == true：一律拒绝；
//   - resolver != nil：始终强制，未声明（declared=false 或取值为空）一律拒绝。
func (c *Manager) resolveScope(ctx context.Context) (value string, enforced bool, err error) {
	if c.scopeResolver == nil {
		if c.requireScope {
			return "", true, gasync.ErrScopeRequired
		}
		return "", false, nil
	}
	v, ok := c.scopeResolver(ctx)
	if !ok || v == "" {
		return "", true, gasync.ErrScopeRequired
	}
	return v, true, nil
}
