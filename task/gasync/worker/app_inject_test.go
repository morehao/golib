package worker_test

import (
	"context"
	"testing"
	"time"

	"github.com/morehao/golib/dbaccess/dbgorm/driver/sqlite"
	"github.com/morehao/golib/dbaccess/gormdao"
	"github.com/morehao/golib/task/gasync"
	gasyncdb "github.com/morehao/golib/task/gasync/source/db"
	"github.com/morehao/golib/task/gasync/worker"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// workerAppProbe 模拟已注册租户回调的下游组件表（fail-closed：ctx 缺 app 报 ErrAppMissing）。
type workerAppProbe struct {
	ID    string `gorm:"column:id;primaryKey;type:varchar(36)"`
	AppID string `gorm:"column:app_id;type:varchar(64);not null;default:''"`
	Val   string `gorm:"column:val;type:varchar(64)"`
}

func (workerAppProbe) TableName() string { return "worker_app_probe" }

// TestWorker_AppInjectedFromAsyncTask 验证 worker 复用既有 ScopeInjector 钩子，把任务携带的
// AppID 注入 handler ctx（经由 source/db.ScopeInjectorWithApp），使 handler 触达下游 app 隔离
// 表时不报 ErrAppMissing——与 gcron 的定时触发保持同一口径，避免双轨。
func TestWorker_AppInjectedFromAsyncTask(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("sqlite://:memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&workerAppProbe{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := gormdao.RegisterTenantScope(db, workerAppProbe{}.TableName()); err != nil {
		t.Fatalf("register scope: %v", err)
	}

	const appID = "biz-app-7"
	src := newMemSource()
	src.push("invoice", &gasync.AsyncTask{ID: "task-app", Type: "invoice", AppID: appID})

	var (
		gotApp    string
		hasApp    bool
		queryErr  error
		createErr error
	)
	h := worker.HandlerFunc(func(ctx context.Context, _ *gasync.AsyncTask) error {
		gotApp, hasApp = gormdao.AppFrom(ctx)
		var rows []workerAppProbe
		queryErr = db.WithContext(ctx).Where("id = ?", "nope").Find(&rows).Error
		createErr = db.WithContext(ctx).Create(&workerAppProbe{ID: "p1", Val: "v"}).Error
		return nil
	})

	w := worker.New(src, &worker.WorkerConfig{
		Concurrency:  1,
		PollInterval: 5 * time.Millisecond,
		// 复用既有 ScopeInjector 钩子：source/db 提供的注入器把 task.AppID → ctx。
		ScopeInjector: gasyncdb.ScopeInjectorWithApp(nil),
	})
	if err := w.Register("invoice", h); err != nil {
		t.Fatalf("register: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()

	waitCond(t, func() bool {
		res, _, _ := src.snapshot()
		return len(res) >= 1
	}, 2*time.Second)

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer stopCancel()
	if err := w.Shutdown(stopCtx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	cancel()
	<-done

	if !hasApp || gotApp != appID {
		t.Fatalf("handler ctx app = %q (has=%v), want %q", gotApp, hasApp, appID)
	}
	if queryErr != nil {
		t.Fatalf("handler 读下游 app 隔离表不应 ErrAppMissing: %v", queryErr)
	}
	if createErr != nil {
		t.Fatalf("handler 写下游 app 隔离表不应 ErrAppMissing: %v", createErr)
	}
}
