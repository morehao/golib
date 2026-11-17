package ginasync

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/morehao/golib/dbaccess/dbgorm/driver/sqlite"
	"github.com/morehao/golib/task/gasync"
	"github.com/morehao/golib/task/gasync/center"
	"github.com/morehao/golib/task/gasync/manager"
	"github.com/morehao/golib/task/gasync/source/remote"
	"github.com/morehao/golib/task/gasync/wire"
	"github.com/morehao/golib/task/gasync/worker"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// centerFixture 是「远程中心 + 远端 worker」的端到端夹具：
// sqlite 内存库 → manager → center → gin 路由 → httptest 服务端 → remote.Source。
type centerFixture struct {
	mgr  *manager.Manager
	base string
	srv  *httptest.Server
}

func newCenterFixture(t *testing.T) *centerFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db, err := gorm.Open(sqlite.Open("sqlite://:memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)

	mgr, err := manager.New(&manager.Config{
		LeaseTTL: 60 * time.Second,
		Backoff:  func(int) time.Duration { return time.Millisecond },
	}, db)
	require.NoError(t, err)

	c, err := center.New(mgr)
	require.NoError(t, err)

	engine := gin.New()
	RegisterCenter(engine.Group("/gasync/center"), c)
	srv := httptest.NewServer(engine)

	return &centerFixture{mgr: mgr, base: srv.URL + "/gasync/center", srv: srv}
}

func (f *centerFixture) close() { f.srv.Close() }

// waitStatus 轮询等到任务进入期望状态。
func (f *centerFixture) waitStatus(t *testing.T, id string, want gasync.Status, timeout time.Duration) *gasync.AsyncTask {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		task, err := f.mgr.Get(context.Background(), id)
		require.NoError(t, err)
		if task.Status == want {
			return task
		}
		if time.Now().After(deadline) {
			t.Fatalf("等待 status=%s 超时，当前 %s", want, task.Status)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestCenterHTTP_RemoteWorkerEndToEnd 远端 worker 通过 HTTP 四段完成一条任务。
func TestCenterHTTP_RemoteWorkerEndToEnd(t *testing.T) {
	f := newCenterFixture(t)
	defer f.close()
	ctx := context.Background()

	task, _, err := f.mgr.Submit(ctx, &gasync.AsyncTaskSpec{
		Type: "remote:demo", Payload: []byte(`{"order":42}`),
	})
	require.NoError(t, err)

	var payloadOK atomic.Bool
	src, err := remote.New(remote.Config{BaseURL: f.base, WorkerID: "w-remote"})
	require.NoError(t, err)
	w := worker.New(src, &worker.WorkerConfig{
		Concurrency:  1,
		PollInterval: 10 * time.Millisecond,
		LeaseTTL:     60 * time.Second,
	})
	require.NoError(t, w.Register("remote:demo", worker.HandlerFunc(func(ctx context.Context, task *gasync.AsyncTask) error {
		var p struct {
			Order int `json:"order"`
		}
		if err := task.Bind(&p); err != nil {
			return worker.ErrNoRetry
		}
		payloadOK.Store(p.Order == 42)
		return nil
	})))
	require.NoError(t, w.Start(ctx))

	got := f.waitStatus(t, task.ID, gasync.StatusSucceeded, 5*time.Second)
	require.True(t, payloadOK.Load(), "远端 worker 应收到完整 payload")
	require.Equal(t, 1, got.Attempt)

	// 尝试记录也要落中心库（认领即建行、回传收尾）。
	_, runs, err := f.mgr.GetRuns(ctx, task.ID)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	require.Equal(t, gasync.StatusSucceeded, runs[0].Status)
	require.Equal(t, "w-remote", runs[0].WorkerID)

	require.NoError(t, w.Shutdown(context.Background()))
}

// TestCenterHTTP_ShutdownReleasesLease 远端停机时通过 /Release 放租约，任务立即能被别人认领。
func TestCenterHTTP_ShutdownReleasesLease(t *testing.T) {
	f := newCenterFixture(t)
	defer f.close()
	ctx := context.Background()

	task, _, err := f.mgr.Submit(ctx, &gasync.AsyncTaskSpec{Type: "remote:block"})
	require.NoError(t, err)

	started := make(chan struct{})
	src, err := remote.New(remote.Config{BaseURL: f.base, WorkerID: "w-remote"})
	require.NoError(t, err)
	w := worker.New(src, &worker.WorkerConfig{
		Concurrency:     1,
		PollInterval:    10 * time.Millisecond,
		ShutdownTimeout: 50 * time.Millisecond, // 短停机超时，触发放租约
	})
	require.NoError(t, w.Register("remote:block", worker.HandlerFunc(func(ctx context.Context, task *gasync.AsyncTask) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	})))
	require.NoError(t, w.Start(ctx))

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("远端 handler 未开始")
	}
	require.NoError(t, w.Shutdown(context.Background()))

	got, err := f.mgr.Get(ctx, task.ID)
	require.NoError(t, err)
	require.Equal(t, "", got.WorkerID, "停机后中心侧应已放租约")

	// 另一个 worker 立即能认领（attempt+1），无需等租约过期。
	next, err := f.mgr.FetchFor(ctx, "remote:block", "w-2")
	require.NoError(t, err)
	require.Equal(t, task.ID, next.ID)
	require.Equal(t, 2, next.Attempt)
}

// TestCenterHTTP_HeartbeatOwnership 非持有者的心跳返回 409（客户端据此取消执行）。
func TestCenterHTTP_HeartbeatOwnership(t *testing.T) {
	f := newCenterFixture(t)
	defer f.close()
	ctx := context.Background()

	task, _, err := f.mgr.Submit(ctx, &gasync.AsyncTaskSpec{Type: "remote:owned"})
	require.NoError(t, err)
	claimed, err := f.mgr.FetchFor(ctx, "remote:owned", "w-owner")
	require.NoError(t, err)
	require.Equal(t, task.ID, claimed.ID)

	status := func(workerID string) int {
		body, _ := json.Marshal(wire.HeartbeatRequest{TaskID: task.ID, WorkerID: workerID})
		resp, err := http.Post(f.base+wire.PathHeartbeat, "application/json", bytes.NewReader(body))
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()
		return resp.StatusCode
	}
	require.Equal(t, http.StatusNoContent, status("w-owner"), "持有者心跳应 204")
	require.Equal(t, http.StatusConflict, status("w-other"), "非持有者应 409")

	// 未实现/非法请求体 → 400。
	resp, err := http.Post(f.base+wire.PathHeartbeat, "application/json", bytes.NewReader([]byte(`{}`)))
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

// TestCenterHTTP_FetchNoTaskReturns204 无任务时返回 204（remote 客户端据此走退避而不是报错）。
func TestCenterHTTP_FetchNoTaskReturns204(t *testing.T) {
	f := newCenterFixture(t)
	defer f.close()

	body, _ := json.Marshal(wire.FetchRequest{TaskType: "nothing", WorkerID: "w1"})
	resp, err := http.Post(f.base+wire.PathFetch, "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusNoContent, resp.StatusCode)
}

// TestCenterHTTP_UnknownPath 确保只注册了协议 4 个端点（管理面路由不会被误挂到中心前缀）。
func TestCenterHTTP_UnknownPath(t *testing.T) {
	f := newCenterFixture(t)
	defer f.close()

	resp, err := http.Get(f.base + "/asyncTasks")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusNotFound, resp.StatusCode, fmt.Sprintf("GET %s 不该存在", f.base+"/asyncTasks"))
}
