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
