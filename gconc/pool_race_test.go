package gconc

import (
	"context"
	"sync"
	"testing"
	"time"
)

// 本文件是两个已修复并发缺陷的回归测试。两者都是"发送方与 worker 之间的
// 时序"缺陷，用单次调用难以稳定复现，因此按轮次循环 —— 循环本身就是断言：
// 修复前能观察到 panic，修复后一次都不应出现。
//
// 若将来有人把计数或队列关闭的顺序改回去，这两个用例会以 panic 失败。
// 用 recover 收集 panic（而不是让进程崩溃），这样失败信息里能带上复现轮次。

// TestRegression_TaskCountReservedBeforeEnqueue 守护 "先 reserve 再入队"。
//
// 缺陷：原先 Submit/Send 是"先入队、后 taskWG.Add(1)"。worker 可能在 Add 之前
// 就取走并执行完该任务、进而调用 taskWG.Done()，使计数变成 -1 而 panic
// （sync: negative WaitGroup counter）。Send 走无缓冲队列时该竞态几乎必然发生。
func TestRegression_TaskCountReservedBeforeEnqueue(t *testing.T) {
	var mu sync.Mutex
	var panics []any
	record := func() {
		if r := recover(); r != nil {
			mu.Lock()
			panics = append(panics, r)
			mu.Unlock()
		}
	}

	const rounds = 500
	for i := 0; i < rounds; i++ {
		p := New(1, 0)
		func() {
			defer record()
			p.Send(func(ctx context.Context) error { return nil })
		}()
		p.Shutdown()
	}

	mu.Lock()
	defer mu.Unlock()
	if len(panics) > 0 {
		t.Fatalf("实测确认回归：%d 轮中 %d 轮 panic，首例=%v", rounds, len(panics), panics[0])
	}
}

// TestRegression_EnqueueRacesWithClose 守护 "关闭队列与在途发送互斥"。
//
// 缺陷：Shutdown/ShutdownNow 直接 close(taskQueue)，而 Send 可能正阻塞在
// "等待队列空位"的发送上，此时关闭队列会让该发送 panic（send on closed channel）。
// 修复方式是用 sendMu 让关闭等待在途发送完成。实测修复前 300 轮复现 1 次。
func TestRegression_EnqueueRacesWithClose(t *testing.T) {
	var mu sync.Mutex
	var panics []any

	const rounds = 500
	for i := 0; i < rounds; i++ {
		// 无缓冲队列 + 唯一 worker：worker 被占住时 Send 必然阻塞。
		p := New(1, 0)
		// 占位任务响应 ctx，使 ShutdownNow 的 cancel 能释放 worker。
		p.Submit(func(ctx context.Context) error { <-ctx.Done(); return nil })
		time.Sleep(time.Millisecond)

		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					mu.Lock()
					panics = append(panics, r)
					mu.Unlock()
				}
			}()
			p.Send(func(ctx context.Context) error { return nil })
		}()

		time.Sleep(200 * time.Microsecond)
		p.ShutdownNow()
		wg.Wait()
	}

	mu.Lock()
	defer mu.Unlock()
	if len(panics) > 0 {
		t.Fatalf("实测确认回归：%d 轮中 %d 轮 panic，首例=%v", rounds, len(panics), panics[0])
	}
}
