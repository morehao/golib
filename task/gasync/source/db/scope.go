package db

import (
	"context"
	"fmt"

	"github.com/morehao/golib/task/gasync"
	"gorm.io/gorm"
)

// 作用域强制（fail-closed）的取值口径。三条分支（见设计 §5.6 与执行计划 T1.1/T1.6）：
//
//   - resolver == nil 且 RequireScope == false：灰度兼容，完全不强制——
//     读不加作用域条件、写落空 scope，行为与加作用域前一致；
//   - resolver == nil 且 RequireScope == true：配置矛盾（要求强制却没给取值来源），
//     一律拒绝（ErrScopeRequired）；
//   - resolver != nil：始终强制——未声明（declared=false 或取值为空）一律拒绝，
//     不受 RequireScope 影响。平台任务用保留值 ScopePlatform（非空）显式声明，不会被误杀。

// resolveScope 解析当前请求的作用域取值并给出是否强制。
func (s *store) resolveScope(ctx context.Context) (value string, enforced bool, err error) {
	if s.scopeResolver == nil {
		if s.requireScope {
			return "", true, gasync.ErrScopeRequired
		}
		return "", false, nil
	}
	v, ok := s.scopeResolver(ctx)
	if !ok || v == "" {
		return "", true, gasync.ErrScopeRequired
	}
	return v, true, nil
}

// applyScope 把作用域条件（scope_key = value）叠加到查询上。
//
// enforced=false（灰度兼容）时原样返回 q、不追加条件。
// 注意：本方法只用于「管理面读路径」，认领（claim）不经过它——认领是全局的（D7）。
func (s *store) applyScope(ctx context.Context, q *gorm.DB) (*gorm.DB, error) {
	value, enforced, err := s.resolveScope(ctx)
	if err != nil {
		return q, err
	}
	if !enforced {
		return q, nil
	}
	return q.Where("scope_key = ?", value), nil
}

// writeScope 返回写入侧应落库的 scope_key：enforced 时取解析值（必非空），否则空串。
func (s *store) writeScope(ctx context.Context) (string, error) {
	value, enforced, err := s.resolveScope(ctx)
	if err != nil {
		return "", err
	}
	if !enforced {
		return "", nil
	}
	return value, nil
}

// applyRunScope 把作用域条件叠加到尝试记录查询上。
//
// 尝试行（core_async_task_run）无 scope_key 列，故经父任务归属约束：
// async_task_id IN (SELECT id FROM core_async_task WHERE scope_key = ?)。
// enforced=false 时不追加条件。
func (s *store) applyRunScope(ctx context.Context, q *gorm.DB) (*gorm.DB, error) {
	value, enforced, err := s.resolveScope(ctx)
	if err != nil {
		return q, err
	}
	if !enforced {
		return q, nil
	}
	sub := fmt.Sprintf("async_task_id IN (SELECT id FROM %s WHERE scope_key = ?)", CoreAsyncTaskTableName)
	return q.Where(sub, value), nil
}
