# GHTTP - 增强的HTTP客户端

这是一个功能丰富的HTTP客户端库，支持结构体映射、连接池、重试机制等高级功能。

## 主要特性

### 1. 结构体映射支持
- 支持将HTTP响应直接映射到Go结构体
- 提供`GetJSON`、`PostJSON`、`PutJSON`、`DeleteJSON`、`PatchJSON`便捷方法
- 支持手动反序列化

### 2. 连接池优化
- 内置HTTP连接池配置
- 支持最大空闲连接数和每主机连接数限制
- 提高并发性能

### 3. 智能重试机制
- 自动重试服务器错误（5xx）
- 客户端错误（4xx）不重试
- 可配置重试次数和延迟
- 支持请求体重试（POST/PUT/PATCH）

### 4. 丰富的响应处理
- `IsSuccess()` - 检查响应是否成功
- `IsError()` - 检查响应是否为错误
- `String()` - 获取响应体字符串
- `Bytes()` - 获取响应体字节数组
- `JSON(v)` - 反序列化到结构体

### 5. 自定义错误类型
- `HTTPError` 提供详细的错误信息
- 区分客户端错误（4xx）和服务器错误（5xx）
- 包含完整的响应信息

## 使用方法

### 基本配置

```go
cfg := &protocol.HttpClientConfig{
    Module:   "my-service",
    Host:     "https://api.example.com",
    Timeout:  10 * time.Second,
    MaxRetry: 3,
}
client := NewClient(cfg)
```

### 基本请求

```go
// GET请求
result, err := client.Get(ctx, "/users/1", RequestOption{})
if err != nil {
    return err
}

// 检查响应状态
if result.IsSuccess() {
    fmt.Printf("响应: %s\n", result.String())
}
```

### 结构体映射

```go
// 定义响应结构体
type User struct {
    ID    int    `json:"id"`
    Name  string `json:"name"`
    Email string `json:"email"`
}

// 直接映射到结构体
var user User
err := client.GetJSON(ctx, "/users/1", &user, RequestOption{})
if err != nil {
    return err
}
fmt.Printf("用户: %+v\n", user)
```

### POST请求

```go
type CreateUserRequest struct {
    Name  string `json:"name"`
    Email string `json:"email"`
}

type CreateUserResponse struct {
    ID      int    `json:"id"`
    Message string `json:"message"`
}

requestData := CreateUserRequest{
    Name:  "张三",
    Email: "zhangsan@example.com",
}

var response CreateUserResponse
err := client.PostJSON(ctx, "/users", &response, RequestOption{
    RequestBody: requestData,
})
```

### PUT/DELETE/PATCH 请求

```go
// PUT 请求
var updateResp UpdateResponse
err := client.PutJSON(ctx, "/users/1", &updateResp, RequestOption{
    RequestBody: updateData,
})

// DELETE 请求
var deleteResp DeleteResponse
err := client.DeleteJSON(ctx, "/users/1", &deleteResp, RequestOption{})

// PATCH 请求
var patchResp PatchResponse
err := client.PatchJSON(ctx, "/users/1", &patchResp, RequestOption{
    RequestBody: patchData,
})
```

### 自定义请求选项

```go
opt := RequestOption{
    Headers: map[string]string{
        "Authorization": "Bearer token123",
        "X-Custom-Header": "custom-value",
    },
    Cookies: map[string]string{
        "session_id": "abc123",
    },
    ContentType: "application/json",
    Timeout:     5 * time.Second,
}

result, err := client.Get(ctx, "/protected-resource", opt)
```

## 改进内容

### 1. 新增功能
- ✅ 添加`JSON()`方法支持结构体映射
- ✅ 添加`IsSuccess()`和`IsError()`状态检查方法
- ✅ 添加`String()`和`Bytes()`响应获取方法
- ✅ 添加`GetJSON()`和`PostJSON()`便捷方法
- ✅ 添加HTTP连接池支持
- ✅ 改进错误处理机制

### 2. 修复问题
- ✅ 修复重试逻辑中的资源泄漏问题
- ✅ 改进错误信息，区分服务器错误和客户端错误
- ✅ 优化连接复用，提高性能

### 3. 测试覆盖
- ✅ 添加结构体映射测试
- ✅ 添加响应方法测试
- ✅ 添加GET/POST JSON测试

## 性能优化

1. **连接池**: 默认配置100个最大空闲连接，每主机10个连接
2. **连接复用**: 避免频繁建立TCP连接
3. **智能重试**: 只对服务器错误进行重试，避免不必要的网络开销
4. **资源管理**: 确保响应体正确关闭，避免内存泄漏

## 错误处理

```go
result, err := client.Get(ctx, "/protected-resource", RequestOption{})
if err != nil {
    // 检查是否为 HTTP 错误
    if httpErr, ok := err.(*HTTPError); ok {
        fmt.Printf("HTTP错误: 状态码=%d, 消息=%s\n", httpErr.HttpCode, httpErr.Message)
        
        if httpErr.IsClientError() {
            fmt.Println("客户端错误，请检查请求参数")
        } else if httpErr.IsServerError() {
            fmt.Println("服务器错误，请稍后重试")
        }
        
        // 可以访问响应体和头部
        fmt.Printf("响应体: %s\n", string(httpErr.Body))
    } else {
        // 网络错误或其他错误
        fmt.Printf("请求失败: %v\n", err)
    }
    return
}

// 处理成功响应
if result.IsSuccess() {
    fmt.Printf("响应: %s\n", result.String())
}
```

## 日志记录

- 自动记录请求和响应信息
- 支持请求ID追踪
- 可配置日志级别
- 限制日志大小，避免日志过大
