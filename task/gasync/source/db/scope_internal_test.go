package db

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/morehao/golib/dbaccess/dbgorm/driver/sqlite"
	"github.com/morehao/golib/task/gasync"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type scopeKey struct{}

func ctxScope(ctx context.Context, scope string) context.Context {
	return context.WithValue(ctx, scopeKey{}, scope)
}

func scopeResolver(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(scopeKey{}).(string)
	if !ok || v == "" {
		return "", false
	}
	return v, true
}

func newScopeSource(t *testing.T) (*source, *gorm.DB) {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open("sqlite://:memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := gdb.AutoMigrate(&CoreAsyncTask{}, &CoreAsyncTaskRun{}, &CoreAsyncTaskType{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	src, err := New(Config{DB: gdb, LeaseTTL: 60 * time.Second, WorkerID: "w-scope", ScopeResolver: scopeResolver})
	if err != nil {
		t.Fatalf("new source: %v", err)
	}
	return src, gdb
}

// TestScope_ClaimIsGlobal 认领不做作用域过滤：任一域任务都能被全局认领（D7/T8 前半）。
func TestScope_ClaimIsGlobal(t *testing.T) {
	src, gdb := newScopeSource(t)
	now := time.Now()
	id := genUUID()
	if err := gdb.Create(&CoreAsyncTask{
		ID: id, CreatedAt: now, UpdatedAt: now, TaskType: "invoice", ScopeKey: "tenantA",
		Status: "pending", Generation: 1, Attempt: 0, MaxAttempts: 3,
		NextRunAt: now.Add(-time.Minute),
	}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	// 用一个“完全无关”的 ctx（未声明作用域）认领，仍应取到 tenantA 的任务。
	task, err := src.Fetch(context.Background(), "invoice")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if task.ID != id || task.Scope != "tenantA" {
		t.Fatalf("claimed task id=%s scope=%q, want id=%s scope=tenantA", task.ID, task.Scope, id)
	}
}

// TestScope_WriteGuardCrossScope 跨域误传任务行 → Save/Renew/Release 守卫拒绝（ErrLeaseLost）。
func TestScope_WriteGuardCrossScope(t *testing.T) {
	src, gdb := newScopeSource(t)
	now := time.Now()
	id := genUUID()
	if err := gdb.Create(&CoreAsyncTask{
		ID: id, CreatedAt: now, UpdatedAt: now, TaskType: "invoice", ScopeKey: "tenantA",
		Status: "pending", Generation: 1, Attempt: 0, MaxAttempts: 3,
		NextRunAt: now.Add(-time.Minute),
	}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	ctx := ctxScope(context.Background(), "tenantA")
	task, err := src.Fetch(ctx, "invoice")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	// 伪装成别的域的任务行（同一 id/worker/generation/attempt，但 scope 不同）。
	forged := *task
	forged.Scope = "tenantB"

	if err := src.Save(ctx, &forged, gasync.Result{Outcome: gasync.OutcomeSucceeded}, nil); !errors.Is(err, gasync.ErrLeaseLost) {
		t.Errorf("Save cross-scope err = %v, want ErrLeaseLost", err)
	}
	if err := src.Renew(ctx, &forged); !errors.Is(err, gasync.ErrLeaseLost) {
		t.Errorf("Renew cross-scope err = %v, want ErrLeaseLost", err)
	}
	if err := src.Release(ctx, &forged); err != nil {
		// Release 是 UPDATE，跨域命中 0 行，不报错是预期的（无副作用）。
		t.Logf("Release cross-scope err = %v (0 rows expected, non-fatal)", err)
	}
	// 原任务行仍为 running（未被跨域写入污染）。
	var got CoreAsyncTask
	if err := gdb.Where("id = ?", id).Take(&got).Error; err != nil {
		t.Fatal(err)
	}
	if got.Status != "running" {
		t.Errorf("original row status = %q after cross-scope attempts, want running", got.Status)
	}
}
