package manager

import (
	"context"
	"testing"

	"github.com/morehao/golib/dbaccess/gormplugin"
	"github.com/morehao/golib/task/gasync"
)

// TestSubmitPersistsAppFromContext 验证提交任务时从创建者 ctx（gormplugin.WithApp）取 app 并
// 持久化到 core_async_task.app_id，读回实体也带 AppID（供 worker 执行时再注入 handler ctx）。
func TestSubmitPersistsAppFromContext(t *testing.T) {
	m := newTestManager(t)

	const appID = "biz-app-9"
	ctx := gormplugin.WithApp(context.Background(), appID)
	task, submitted, err := m.Submit(ctx, &gasync.AsyncTaskSpec{Type: "invoice"})
	if err != nil || !submitted || task == nil {
		t.Fatalf("Submit: submitted=%v task=%v err=%v", submitted, task, err)
	}
	if task.AppID != appID {
		t.Fatalf("submitted task AppID = %q, want %q", task.AppID, appID)
	}

	// 直接查库确认 app_id 落列。
	var row struct{ AppID string }
	if err := m.db.Table("core_async_task").Select("app_id").Where("id = ?", task.ID).Scan(&row).Error; err != nil {
		t.Fatalf("query app_id: %v", err)
	}
	if row.AppID != appID {
		t.Fatalf("db app_id = %q, want %q", row.AppID, appID)
	}

	// 读回实体（认领/Get 路径走的转换）也应带 AppID。
	got, err := m.Get(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetAsyncTask: %v", err)
	}
	if got.AppID != appID {
		t.Fatalf("read-back AppID = %q, want %q", got.AppID, appID)
	}
}

// TestSubmitWithoutAppLeavesEmpty 验证无 app 上下文的旧调用仍可提交，app_id 落空串（向后兼容）。
func TestSubmitWithoutAppLeavesEmpty(t *testing.T) {
	m := newTestManager(t)
	task, submitted, err := m.Submit(context.Background(), &gasync.AsyncTaskSpec{Type: "invoice"})
	if err != nil || !submitted || task == nil {
		t.Fatalf("Submit: submitted=%v task=%v err=%v", submitted, task, err)
	}
	if task.AppID != "" {
		t.Fatalf("AppID = %q, want empty", task.AppID)
	}
}
