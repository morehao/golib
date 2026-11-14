package gcache_test

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/morehao/golib/gcache"
	_ "github.com/morehao/golib/gcache/memory"
)

// Example 演示 memory 后端的基本读写与 GetOrLoad。
func Example() {
	ctx := context.Background()
	c, err := gcache.New(gcache.Config{Backend: "memory", KeyPrefix: "ex:"})
	if err != nil {
		fmt.Println("new:", err)
		return
	}
	defer c.Close()

	_ = c.Set(ctx, "greeting", "hello", 0)
	var s string
	_ = c.Get(ctx, "greeting", &s)
	fmt.Println(s)

	// 未命中返回 ErrMiss
	var miss string
	err = c.Get(ctx, "absent", &miss)
	fmt.Println("miss:", errors.Is(err, gcache.ErrMiss))

	// GetOrLoad：未命中时回源并写回
	var n int
	_ = c.GetOrLoad(ctx, "counter", &n, time.Minute, func(context.Context) (any, error) {
		return 42, nil
	})
	fmt.Println("loaded:", n)

	// Output:
	// hello
	// miss: true
	// loaded: 42
}

// ExampleGetAs 演示泛型便捷读。
func ExampleGetAs() {
	ctx := context.Background()
	c, _ := gcache.New(gcache.Config{Backend: "memory"})
	defer c.Close()

	type User struct{ Name string }
	_ = c.Set(ctx, "u:1", User{Name: "李四"}, 0)

	u, ok, _ := gcache.GetAs[User](ctx, c, "u:1")
	fmt.Println(ok, u.Name)

	// Output:
	// true 李四
}
