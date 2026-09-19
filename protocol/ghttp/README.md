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
- 默认仅当网络错误时自动重试（超时、DNS解析失败、连接被拒绝等）
- 通过 `RetryOnStatus` 可选配置按 HTTP 状态码重试（如 429、5xx）
- 通过 `Retryable` 可关闭网络错误重试（默认开启）
- 重试间隔通过 `RetryInterval` 配置，呈指数退避（`base * 2^(n-1)`，封顶 1s）
- 重试等待可被 context 取消，不会阻塞积压请求
- 支持请求体重试（POST/PUT/PATCH）
- `MaxRetry` 表示**总尝试次数（含首次）**：`MaxRetry=3` 即最多请求 3 次（1 次初始 + 2 次重试），`<=0` 视为 1 次

### 4. 丰富的响应处理
- `IsSuccess()` - 检查响应是否成功
- `IsError()` - 检查响应是否为错误
- `String()` - 获取**已缓冲**的响应体字符串（纯访问器，流式模式下为空，需先 `Buffer()`）
- `Bytes()` - 获取**已缓冲**的响应体字节数组（同上）
- `Buffer()` - 把流式响应体整体缓冲进内存（受 `MaxResponseBytes` 约束，可拿到具体错误）
- `JSON(v)` - 反序列化到结构体（流式模式下会按需缓冲，受 `MaxResponseBytes` 约束）（流式模式下会按需缓冲，受 `MaxResponseBytes` 约束）

### 5. 自定义错误类型
- `HTTPError` 提供详细的错误信息
- 区分客户端错误（4xx）和服务器错误（5xx）
- 包含完整的响应信息

### 6. 缓冲上限保护
- `MaxResponseBytes` 限制单次**缓冲进内存**的字节数（默认不限制，配置后生效），超限返回 `ErrResponseTooLarge`
- 超限**整体丢弃**而不是静默截断，避免下游把"被上限拦下"误报成"JSON 解析失败"
- 覆盖 `Result` 路径、`Result.Buffer()` 与错误页；流式消费（`Read`/`io.Copy`）不缓冲、不受限
- 因此"同一接口可能是 JSON、也可能是文件下载"无需靠 `Content-Type` 猜是否设防，按消费方式选路径即可
- 消费模式走选项模式：默认整包，`WithStream()` 切流式；单次上限可用 `WithMaxResponseBytes(n)` 覆盖

## 使用方法

### 基本配置

```go
// Retryable 为 *bool，默认 true（网络错误重试开启），仅显式 false 时关闭。
retryable := true
cfg := &protocol.HttpClientConfig{
    Module:           "my-service",
    Host:             "https://api.example.com",
    Timeout:          10 * time.Second,
    MaxRetry:         3,
    RetryInterval:    200 * time.Millisecond, // 基础重试间隔，默认 100ms，指数退避
    RetryOnStatus:    []int{429, 502, 503},   // 可选：按状态码重试
    Retryable:        &retryable,             // 可选：网络错误是否重试，默认 true
    IdleConnTimeout:  90 * time.Second,       // 可选：空闲连接回收时间，默认 90s
    MaxResponseBytes: 32 << 20,               // 可选：缓冲进内存的字节上限，默认不限制；建议按上游可信度显式设置
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

> **查询参数约束**：GET/HEAD/DELETE 的 `RequestBody` 作为 URL 查询参数时仅支持
> `map[string]string` 或 `map[string]interface{}`，传入其他类型（如 struct）会返回明确错误。
>
> **流式超时说明**：流式响应（`WithStream()`）的 `Timeout` 仅限制「连接 + 响应头」阶段，
> 一旦响应头返回，响应体读取不再受超时约束（由 `ResponseHeaderTimeout` 实现），
> 因此长期存活的 SSE 流不会被截断。如需取消读取，调用 `Result.Close()`。

### 响应体上限

`MaxResponseBytes` 用来兜住"上游返回异常巨大响应"（网关劫持、`base_url` 配错成静态站点等）把内存吃光的场景。

**默认不限制**——与 `net/http`、resty 等主流客户端一致：上限是**显式配置项**，不是默认策略；
面向不可信或易配错的上游时建议显式设置它（或按单次调用用 `WithMaxResponseBytes`）。
需要保护时它约束的是「缓冲进内存的字节数」，不是「响应的字节数」，这是理解它的唯一规则：

| 消费方式 | 是否缓冲 | 上限（配置后） |
| --- | --- | --- |
| 默认：`Get`/`Post`/… → `Result.Response` | 整包 | ✅ 生效 |
| `WithStream()` + `Result.Buffer()` / `Result.JSON()` | 整包（按需） | ✅ 生效 |
| 错误页读取（含流式） | 整包 | ✅ 生效（要塞进 `HTTPError`） |
| `WithStream()` + `Result.Read` / `io.Copy(dst, result)` | 不缓冲 | — 内存恒定，没有"缓冲上限"这回事 |
| `WithStream()` + `Result.Bytes()` / `Result.String()` | 不读网络 | — 纯访问器，只返回已缓冲内容 |

取值：`正数` = 上限字节数；`0`（含未设置）或`负数` = 不限制（默认）。
超限**整体丢弃**内容并返回 `ErrResponseTooLarge`，不会截断成半截 JSON 丢给下游（那只会让排障的人看到"解析失败"）。
按单次调用覆盖用 `WithMaxResponseBytes(n)`（传 0/负数即为本次不限制；重试路径的错误页同样遵循它）。

```go
// 显式设置上限（面向不可信/易配错的上游时建议这么做）
cfg.MaxResponseBytes = 32 << 20

result, err := client.Get(ctx, "/api/data", RequestOption{})
if errors.Is(err, ghttp.ErrResponseTooLarge) {
    // 仅在配置了上限时可能出现：响应超过缓冲上限，内容已被整体丢弃，
    // 而不是"上游返回了空 body"
    log.Printf("response too large: %v", err)
}
```

#### 默认整包，需要流式就加选项

请求数据用 `RequestOption` 结构体，单次调用的行为用选项（与 `glog`/`gasync` 的约定一致）：
**不传选项就是整包**，传 `WithStream()` 才是流式。

```go
// 默认：整包缓冲，受 MaxResponseBytes 约束
result, err := client.Get(ctx, "/api/data", RequestOption{})
var out Resp
err = result.JSON(&out)

// 流式：不缓冲、内存恒定，适合文件下载与原样转发
result, err := client.Get(ctx, "/file/export", RequestOption{}, ghttp.WithStream())
if err != nil {
    return err
}
defer result.Close() // 流式模式必须释放连接（整包模式为空操作）
_, err = io.Copy(w, result)
```

#### 同一个调用点可能是接口、也可能是文件下载

不需要靠猜，也不需要看上游声明的 `Content-Type` 决定"要不要设防"：**只让"怎么消费"分支**即可。

```go
result, err := client.Get(ctx, path, opt, ghttp.WithStream()) // 先统一不缓冲
if err != nil {
    return err // 4xx/5xx 仍是 *HTTPError，错误页已受上限保护
}
defer result.Close()

if isJSON(result.Header.Get("Content-Type")) { // 功能性判断：撒谎只导致解析失败
    var out Resp
    err = result.JSON(&out) // 需要整包时按需缓冲，同样吃 MaxResponseBytes
} else {
    _, err = io.Copy(dst, result) // 不缓冲 ⇒ 不受限
}
```

> `Result.Buffer()`/`Result.JSON()` 需要整体内容，在流式模式下会就地缓冲，失败原因可用 `errors.Is(err, ghttp.ErrResponseTooLarge)` 判定；
> 但一旦用 `Read`/`io.Copy` 消费过，再整体访问会返回 `ErrNotBuffered`——宁可报错，也不把半截内容当成完整响应。

> `Result.Bytes()`/`Result.String()` 是**纯访问器**：只返回已经缓冲好的内容，流式模式下为空，不会替你去读网络。
> 这样"取个字段看看"不会变成一次隐式的整体读取（与 resty 的 `Response.Body()` 与 `RawBody()`/`String()` 分工一致）。
> 需要内容时用 `Buffer()`/`JSON()`，它们会给你具体错误。

> 旧的 `GetStream`/`PostStream`/`StreamResult`/`ToResult` 仍可用，但已废弃：统一用 `WithStream()` + `Result.Buffer()`。

> `Content-Type` 只用来决定**怎么消费**，永远不要用它决定**是否设防**——
> 后者等于把开关交给要防的一方。`Content-Length` 同理，且压缩响应下它对应的是压缩后大小，不能当缓冲量的依据。

> **错误状态码路径**：按 `RetryOnStatus` 重试耗尽、以及流式错误页这两条路径返回的仍是 `*HTTPError`
> （对调用方来说状态码才是首要信息），被上限拦下的原因不会顶掉它，而是挂在 `HTTPError.BodyErr` 上：
> 此时 `HTTPError.Body` 为空，可用 `errors.Is(err, ghttp.ErrResponseTooLarge)` 区分"空错误页"与"被丢弃"。

> **上限解决 OOM，不解决"永远传不完"**：流式场景的超时/取消由 `ctx` 与 `Result.Close()` 负责，
> 与大小上限是两件事。

## 改进内容

### 1. 新增功能
- ✅ 添加`JSON()`方法支持结构体映射
- ✅ 添加`IsSuccess()`和`IsError()`状态检查方法
- ✅ 添加`String()`和`Bytes()`响应获取方法
- ✅ 添加`GetJSON()`和`PostJSON()`便捷方法
- ✅ 添加HTTP连接池支持
- ✅ 改进错误处理机制
- ✅ 添加响应体缓冲上限保护（`MaxResponseBytes` / `WithMaxResponseBytes`）
- ✅ 添加选项模式的消费方式切换（默认整包，`WithStream()` 流式）

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
3. **智能重试**: 仅网络错误时重试，避免不必要的网络开销
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

        // 错误页超过 MaxResponseBytes 时 Body 为空（内容被整体丢弃），原因在 BodyErr
        if errors.Is(httpErr.BodyErr, ghttp.ErrResponseTooLarge) {
            fmt.Println("错误页过大，已被丢弃")
        }
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
