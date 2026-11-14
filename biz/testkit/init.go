// Package testkit 是测试环境初始化工具包（按 appName 注册初始化器 + 构造测试 Gin 上下文）。
//
// Deprecated: 新代码请改用 gtest（gtest/gtest、gtest/httptest、gtest/dbtest、
// gtest/storagetest）。v1.34.5 的 #134 曾直接删除本包，但已发布的下游仍在 import：
//
//   - github.com/morehao/ark-iam/pkg@v0.4.1（testsetup）
//   - github.com/morehao/ark-iam/apps/auth@v0.1.0（testutil）
//
// 这些版本无法通过「下游自己改」来消除引用，会让任何依赖它们的项目 `go mod tidy`
// 直接失败（module 里找不到包）。因此本包自 v1.34.6 起仅作**兼容保留**，实现与 v1.34.4
// 一致、不再演进；待下游全部迁到 gtest 后随下一个主版本移除。
package testkit

import (
	"fmt"
	"sync"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

var (
	initializers = make(map[string]Initializer)
	initOnce     = make(map[string]*sync.Once)
	mu           sync.RWMutex
)

type Initializer interface {
	Initialize() error
	Close() error
}

type InitializerFunc func() (Initializer, error)

var initializerFuncMap = make(map[string]InitializerFunc)

func RegisterInitializer(appName string, initFunc InitializerFunc) {
	mu.Lock()
	defer mu.Unlock()

	if _, exists := initializerFuncMap[appName]; exists {
		panic(fmt.Sprintf("app %s already registered", appName))
	}
	initializerFuncMap[appName] = initFunc
}

func Initialize(appName string) {
	mu.Lock()
	once, ok := initOnce[appName]
	if !ok {
		once = &sync.Once{}
		initOnce[appName] = once
	}
	mu.Unlock()

	once.Do(func() {
		mu.RLock()
		initFunc, ok := initializerFuncMap[appName]
		mu.RUnlock()

		if !ok {
			panic(fmt.Sprintf("app %s not registered", appName))
		}

		initializer, err := initFunc()
		if err != nil {
			panic(fmt.Sprintf("create initializer for app %s failed: %v", appName, err))
		}

		if err := initializer.Initialize(); err != nil {
			panic(fmt.Sprintf("initialize app %s failed: %v", appName, err))
		}

		mu.Lock()
		initializers[appName] = initializer
		mu.Unlock()
	})
}

func Close(appName string) {
	if appName != "" {
		mu.RLock()
		initializer, ok := initializers[appName]
		mu.RUnlock()

		if !ok {
			return
		}

		_ = initializer.Close()

		mu.Lock()
		delete(initializers, appName)
		delete(initOnce, appName)
		mu.Unlock()
	} else {
		mu.Lock()
		defer mu.Unlock()

		for _, initializer := range initializers {
			_ = initializer.Close()
		}

		initializers = make(map[string]Initializer)
		initOnce = make(map[string]*sync.Once)
	}
}

func Init(appName string) {
	Initialize(appName)
}

func Done(appName string) {
	Close(appName)
}
