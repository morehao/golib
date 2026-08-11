package lifecycle

import (
	"context"
	"io"
	"sort"
	"sync"

	"github.com/morehao/golib/glog"
)

type closerItem struct {
	stage int
	c     io.Closer
}

type closerSet struct {
	mu    sync.Mutex
	items []closerItem
	once  sync.Once
	done  chan struct{}
}

func newCloserSet() *closerSet {
	return &closerSet{done: make(chan struct{})}
}

func (s *closerSet) add(stage int, c io.Closer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items = append(s.items, closerItem{stage: stage, c: c})
}

// run 按 stage 升序执行所有收尾动作；stage 相同则按注册顺序。
// 返回的 done 在整个收尾完成时关闭，供外部等待。
func (s *closerSet) run() <-chan struct{} {
	s.once.Do(func() {
		go s.execute()
	})
	return s.done
}

func (s *closerSet) execute() {
	defer close(s.done)

	s.mu.Lock()
	items := make([]closerItem, len(s.items))
	copy(items, s.items)
	s.mu.Unlock()

	// 按 stage 升序排序，保证先关 server 再关资源
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].stage < items[j].stage
	})

	// 相同 stage 内并发执行，提升收尾效率
	var wg sync.WaitGroup
	for i := 0; i < len(items); {
		j := i
		for j+1 < len(items) && items[j+1].stage == items[i].stage {
			j++
		}
		group := items[i : j+1]

		wg.Add(len(group))
		for _, it := range group {
			go func(it closerItem) {
				defer wg.Done()
				if err := safeClose(it.c); err != nil {
					glog.Warnf(context.Background(), "lifecycle: close: %v", err)
				}
			}(it)
		}
		wg.Wait()
		i = j + 1
	}
}

// safeClose 捕获 Close 过程中的 panic。
func safeClose(c io.Closer) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = panicErr(r)
		}
	}()
	return c.Close()
}
