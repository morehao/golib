package gcron

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/morehao/golib/dbaccess/gormplugin"
	"github.com/stretchr/testify/require"
)

// tenantProbe 是回归用例用的「替身组件表」：模拟 dict/gsetting/gprompt/filestore 这类
// 已注册 gormplugin.EnableAppScope 的业务表（fail-closed：ctx 缺 app 时报 ErrAppMissing）。
//
// 不直接 import 真实四包：它们的表结构与改造在多 app 上线期内仍可能变动，耦合会拖垮本 lane；
// 这里用一张结构等价的最小表复现「handler 触下游 app 隔离表」的关键机制即可。
type tenantProbe struct {
	ID    string `gorm:"column:id;primaryKey;type:varchar(36)"`
	AppID string `gorm:"column:app_id;type:varchar(64);not null;default:''"`
	Val   string `gorm:"column:val;type:varchar(64)"`
}

func (tenantProbe) TableName() string { return "tenant_probe" }

// TestCronHandlerCarriesAppContext 验证定时任务 handler 执行时 ctx 携带 app（来自任务自身
// 持久化的 AppID），从而触达下游 app 隔离表时不报 ErrAppMissing——这是多 app 上线最关键的
// 回归点（缺陷 6：gcron 从 background ctx 起链，handler 触四包表会全量 fail-closed）。
func TestCronHandlerCarriesAppContext(t *testing.T) {
	db := newTestDB(t)
	require.NoError(t, AutoMigrate(db))
	// 建替身表并注册租户回调：读/写缺 app 会 fail-closed 返回 ErrAppMissing。
	require.NoError(t, db.AutoMigrate(&tenantProbe{}))
	require.NoError(t, gormplugin.EnableAppScope(db, tenantProbe{}.TableName()))

	const appID = "biz-app-42"
	var (
		mu        sync.Mutex
		gotApp    string
		hasApp    bool
		queryErr  error
		createErr error
		done      = make(chan struct{}, 1)
	)

	s, err := New(db, nil, WithSeconds(true))
	require.NoError(t, err)
	require.NoError(t, s.Register(Task{
		ID:       "app-probe",
		TaskType: "report",
		AppID:    appID, // 注册时持久化到 CronTask.AppID，定时触发据此注入 ctx
		Spec:     "* * * * * *",
		Handler: func(ctx context.Context) error {
			mu.Lock()
			defer mu.Unlock()
			gotApp, hasApp = gormplugin.AppFrom(ctx)
			// 读：缺 app 时 fail-closed 报 ErrAppMissing。
			var rows []tenantProbe
			queryErr = db.WithContext(ctx).Where("id = ?", "nope").Find(&rows).Error
			// 写：缺 app 时 Create 回调 setAppField 前就 AddError(ErrAppMissing)。
			createErr = db.WithContext(ctx).Create(&tenantProbe{ID: "p1", Val: "v"}).Error
			select {
			case done <- struct{}{}:
			default:
			}
			return nil
		},
	}))

	s.Start()
	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	select {
	case <-done:
	case <-time.After(cronWaitTimeout):
		t.Fatal("cron handler did not run within timeout")
	}

	mu.Lock()
	defer mu.Unlock()
	require.True(t, hasApp, "handler ctx 必须携带 app")
	require.Equal(t, appID, gotApp)
	require.NoError(t, queryErr, "handler 读下游 app 隔离表不应 ErrAppMissing")
	require.NoError(t, createErr, "handler 写下游 app 隔离表不应 ErrAppMissing")

	// app 已持久化到任务定义并落到执行记录上。
	def, err := s.GetStore().GetTaskByID(context.Background(), "app-probe")
	require.NoError(t, err)
	require.Equal(t, appID, def.AppID)
	runs, _, err := s.GetStore().ListRun(context.Background(), &CronTaskRunCond{TaskID: "app-probe"})
	require.NoError(t, err)
	require.NotEmpty(t, runs)
	require.Equal(t, appID, runs[0].AppID)
}

// TestCronWithoutAppDoesNotBreak 验证未设置 AppID 的旧任务仍可正常运行（不注入 app，
// 与无 app 上下文的既有行为一致；本用例确保改造对单 app/旧调用向后兼容）。
func TestCronWithoutAppRuns(t *testing.T) {
	db := newTestDB(t)
	require.NoError(t, AutoMigrate(db))

	var ran int32
	s, err := New(db, nil, WithSeconds(true))
	require.NoError(t, err)
	require.NoError(t, s.Register(Task{
		ID:       "no-app",
		TaskType: "report",
		Spec:     "* * * * * *",
		Handler: func(ctx context.Context) error {
			if _, ok := gormplugin.AppFrom(ctx); ok {
				t.Errorf("未设置 AppID 的任务不应携带 app")
			}
			ran = 1
			return nil
		},
	}))
	s.Start()
	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	require.Eventually(t, func() bool { return ran == 1 }, cronWaitTimeout, 20*time.Millisecond)
}
