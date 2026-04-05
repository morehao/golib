# TestKit

测试工具包，用于简化单元测试和集成测试的初始化工作。

## 快速开始

### 1. 注册初始化器

在各服务的 config 或 testkit 包中注册初始化器：

```go
// services/myservice/config/initializer.go
package config

import (
    "os"
    
    "github.com/morehao/golib/biz/testkit"
    "github.com/morehao/golib/glog"
)

type myserviceInitializer struct {
    *testkit.BaseInitializer
}

func newMyServiceInitializer() (testkit.Initializer, error) {
    base, err := testkit.NewBaseInitializer("myservice")
    if err != nil {
        return nil, err
    }
    return &myserviceInitializer{BaseInitializer: base}, nil
}

func (m *myserviceInitializer) Initialize() error {
    // 1. 查找配置文件
    configPath := m.FindConfigPath() // 或自定义路径
    
    // 2. 加载配置
    LoadConfig(configPath)
    
    // 3. 初始化日志
    logCfg, _ := Conf.Log["default"]
    glog.InitLogger(&logCfg)
    
    // 4. 初始化其他资源
    // ...
    
    return nil
}

func (m *myserviceInitializer) Close() error {
    glog.Close()
    return nil
}

func init() {
    testkit.RegisterInitializer("myservice", newMyServiceInitializer)
}
```

### 2. 在 TestMain 中使用

```go
package myservice_test

import (
    "os"
    "testing"
    
    "github.com/morehao/golib/biz/testkit"
)

func TestMain(m *testing.M) {
    testkit.Initialize("myservice")
    
    code := m.Run()
    
    testkit.Close("myservice")
    
    os.Exit(code)
}
```

### 3. 在测试中使用

```go
func TestYourFunction(t *testing.T) {
    ctx := testkit.NewContext(
        testkit.WithUserID(123),
        testkit.WithCompanyID(1),
        testkit.WithMethod("POST"),
        testkit.WithURL("/api/users"),
        testkit.WithJSON(),
    )
    
    result := myService.DoSomething(ctx)
    // ...
}
```

## 上下文选项

```go
ctx := testkit.NewContext(
    // 基础选项
    testkit.WithUserID(123),
    testkit.WithCompanyID(1),
    testkit.WithRequestID("test-req-001"),
    testkit.WithKeyValue("key", "value"),
    
    // HTTP 请求
    testkit.WithMethod("POST"),
    testkit.WithURL("/api/users"),
    testkit.WithQueryParams(map[string]string{
        "page": "1",
        "size": "10",
    }),
    
    // 内容类型
    testkit.WithJSON(),
    testkit.WithFormData(),
    testkit.WithMultipartFormData(),
    
    // 认证
    testkit.WithBearerToken("token"),
    testkit.WithClientIP("127.0.0.1"),
    
    // 请求体
    testkit.WithBody([]byte(`{"name":"test"}`)),
    
    // 自定义请求头
    testkit.WithHeader("X-Custom-Header", "value"),
)
```

## 设计原则

1. **灵活性**：
   - 不硬编码任何服务的初始化逻辑
   - 各服务通过 `RegisterInitializer` 自主注册
   - 配置路径、初始化的资源完全由使用方控制

2. **幂等性**：
   - 每个应用只会初始化一次
   - 线程安全的全局状态管理

3. **选项模式**：
   - `NewContext` 使用函数选项模式
   - 灵活且易于扩展