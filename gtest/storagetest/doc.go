// Package storagetest 提供 storage 领域的可复用测试资产：进程内 S3 协议桩、
// 完整语义内存实现，以及由一批 Case 组成的驱动无关契约。
//
// 契约以 []Case 表达：每个 Case 自声明所需能力（Requires），RunCases 逐个 t.Run，
// 缺能力则 t.Skip。这是 Go 官方 conformance 范式（database/sql/driver、net/http）
// 的轻量版本，不引入结构体框架。
package storagetest

import (
	"context"
	"testing"

	"github.com/morehao/golib/storage"
)

// Spec 描述被验证的存储实现与测试环境。
type Spec struct {
	S      storage.Storage
	Bucket string
	Bulk   bool // 是否跑大体积/超批量用例（替代 STORAGE_SUITE_BULK）
}

// Case 是一条契约用例。
type Case struct {
	Name     string
	Requires func(storage.Caps) bool
	Run      func(t *testing.T, s Spec)
}

// RunCases 运行一批用例。
//
// 流程：对每个 Case 起一个 t.Run；在子测试内先注册 suiteKeys 清理（保证任何
// Case 失败或中途 t.Fatal 都能回收本次写入的对象），再按 Requires + Caps
// 决定跳过还是执行。跳过信息统一带 [case=Name] 前缀。
func RunCases(t *testing.T, s Spec, cases ...Case) {
	t.Helper()
	for _, c := range cases {
		c := c
		t.Run(c.Name, func(t *testing.T) {
			t.Helper()
			// 清理收尾：每个 Case 独立注册，LIFO 保证在子测试结束后执行。
			t.Cleanup(func() {
				if err := s.S.DeleteObjects(context.Background(), s.Bucket, suiteKeys); err != nil {
					t.Logf("清理测试对象失败（不影响测试结论）: %v", err)
				}
			})
			caps := s.S.Caps()
			if c.Requires != nil && !c.Requires(caps) {
				t.Skipf("[case=%s] 后端能力不满足，跳过: %+v", c.Name, caps)
				return
			}
			c.Run(t, s)
		})
	}
}
