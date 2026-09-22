# gllm/driver/openai

把 eino-ext 的 OpenAI 兼容组件接入 `gllm`，注册名为 `openai`。

## 覆盖范围

**一个驱动覆盖所有 OpenAI 兼容端点**，不需要为每家厂商写一个驱动：

| 场景 | `base_url` 示例 |
|---|---|
| OpenAI 官方 | `https://api.openai.com/v1` |
| DeepSeek | `https://api.deepseek.com` |
| 阿里云百炼 / DashScope 兼容模式 | `https://dashscope.aliyuncs.com/compatible-mode/v1` |
| 火山方舟 Ark | `https://ark.cn-beijing.volces.com/api/v3` |
| 本地 vLLM / Ollama | `http://127.0.0.1:8000/v1` |

`base_url` **原样使用**：驱动不做拼接也不做剥离，**按厂商文档写全**——上面几家里
DeepSeek 官方给的就是不带 `/v1` 的那个，其余多在末尾带一段版本路径。
路径写少了会 404 / `path not found`。

## 用法

```go
import _ "github.com/morehao/golib/gllm/driver/openai"
```

blank import 即可，无需其它调用。

```go
gllm.Provider{
    Type:    "openai",
    BaseURL: "https://api.deepseek.com",
    APIKey:  os.Getenv("DEEPSEEK_API_KEY"),
    Timeout: 60 * time.Second,
    Headers: map[string]string{"X-Trace-Id": "abc"}, // 可选
}
```

## 字段映射

| `gllm` 字段 | eino-ext 字段 | 说明 |
|---|---|---|
| `Provider.APIKey` | `APIKey` | |
| `Provider.BaseURL` | `BaseURL` | 原样传递 |
| `Provider.Timeout` | `Timeout` | 未配置 `Headers` 时 |
| `Provider.Timeout` | `HTTPClient.Timeout` | 配置了 `Headers` 时（见下） |
| `Provider.Headers` | `HTTPClient.Transport` | eino-ext 无 headers 字段，通过自定义 `RoundTripper` 注入 |
| `Resolved.ModelName` | `Model` | 取自 `Config.Models` 的 key，原样传递 |
| `ModelConfig.Temperature` | `Temperature` | 指针，未设置时不传 |
| `ModelConfig.MaxTokens` | `MaxTokens` | **不是** `MaxCompletionTokens`，见下 |
| `ModelConfig.Extra` | `ExtraFields` | 原样并入请求体，同名键覆盖基础字段 |

### 为什么 `max_tokens` 不映射到 `max_completion_tokens`

eino-ext 把 `MaxTokens` 标为 deprecated 并推荐 `MaxCompletionTokens`，但**实测结论是相反**：

| 请求字段 | `completion_tokens` | `finish_reason` |
|---|---|---|
| `max_tokens: 32` | 32 | `length` ✅ 生效 |
| `max_completion_tokens: 32` | **8660** | `stop` ❌ **被忽略** |

（在 `llm.yygu.cn` 的 `deepseek-flash` 上实测。忽略该字段不会报错，只会让长度限制
静默失效——这比使用 deprecated 字段危险得多，所以选择按字面映射。）

需要 `max_completion_tokens` 的模型（OpenAI o1/o3 系列）用 `extra` 覆盖：

```go
gllm.Config{
    Providers: map[string]gllm.Provider{
        "official": {Type: "openai", BaseURL: "https://api.openai.com/v1"},
    },
    Models: map[string]gllm.ModelConfig{
        "o3": {
            Provider: "official",
            Extra:    map[string]any{"max_completion_tokens": 4096},
        },
    },
}
```

### 为什么 Headers 要换掉 HTTPClient

eino-ext 的 `ChatModelConfig` 没有 headers 字段，只能通过 `HTTPClient` 注入。
但**一旦设置 `HTTPClient`，组件自身的 `Timeout` 就失效了**，所以驱动必须把
超时改挂到 `http.Client.Timeout` 上，否则配置里的 `timeout` 会被静默忽略。

这也是本驱动唯一一段非平凡逻辑（约 20 行）。注入用的 `RoundTripper` 会克隆请求后
再附加头，符合 `http.RoundTripper` 不得修改入参的约定。

## 能力声明

```go
gllm.WithCapability(gllm.Capability{Tools: true, Vision: true})
```

**`Reasoning` 有意不声明**，尽管实测 `deepseek-flash` 会返回 `reasoning_content`
（甚至推理 token 会先吃掉 `max_tokens` 预算，实测 39 prompt / 16 reasoning）。

原因是能力表**按驱动类型**登记，而 reasoning 支持**按模型**变化：`deepseek-flash`
返回推理内容，同类型的 OpenAI `gpt-4o` 不返回。在 `openai` 这一类上声明 `Reasoning: true`
会对官方端点撒谎。

**`Vision` 已经踩到同一个坑**：这里声明了 `Vision: true`，但同一个 DeepSeek 账号下
`deepseek-v4-pro` 并不支持图像理解（只有 `deepseek-flash` 支持）。所以
`Supports(type, ...)` 只能用来挡住「这套驱动不支持」，不能用来判断某个具体模型支持什么——
多模型接入前请按厂商文档逐个核对能力，别依赖能力表。

这是当前能力表的已知局限：**粒度是驱动类型，不是模型**。若有项目真的需要按模型查询
推理能力，应扩展能力表（例如允许驱动按模型名动态回答），而不是在这里硬编码。
在那之前，调用方按需读取 `schema.Message.ReasoningContent` 自行判断是否为空。

## 测试

`driver_test.go` 用 `httptest` 起本地假上游做端到端验证（不发真实网络），
断言路径、鉴权头、自定义头与请求体字段。

```bash
go test -race ./gllm/driver/openai/...
```
