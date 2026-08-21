# llm - 多供应商 LLM API 统一调用组件

`llm` 是 golib 下针对「调用各供应商、各协议 LLM API」封装的统一组件。

参考了 [`new-api`](https://github.com/QuantumNous/new-api) 的 `relay/channel` 设计思想，
但**砍掉网关层**（计费、额度、多 key 负载均衡、请求路由），只保留核心：
**「把一份统一请求翻译成各供应商协议并发出调用」**。

## 设计思路

### 核心抽象：Provider

与 new-api `Adaptor`「一个巨型接口挂所有方法」不同，本组件按**能力**拆分接口。
每个供应商用一个 provider 实现，负责「自身协议 <-> 统一 `dto` 协议」的双向映射，
只实现自己用得到的能力。

```go
type Provider interface {
    Name() string
    Chat(ctx, httpClient *ghttp.Client, apiKey string, req *dto.ChatRequest) (*dto.ChatResponse, error)
    ChatStream(ctx, httpClient *ghttp.Client, apiKey string, req *dto.ChatRequest, handler func(*dto.StreamChunk) error) error
}
```

### 统一协议 = OpenAI Chat Completions

`llm/dto` 以 **OpenAI Chat Completions 协议**为内部统一基准。调用方永远面向这套结构，
供应商差异完全收敛在 provider 层。

- 对 **OpenAI 兼容**供应商（deepseek / siliconflow / moonshot / 各类中转站），
  统一 dto 本身就是上游协议，无需转换——本仓库的 `openai` provider 直接覆盖。
- 对**异协议**供应商（anthropic / gemini 等），由对应 provider 做字段映射。

> 这也意味着 `openai` provider + `BaseURL/模型名` 配置化，即可打通绝大多数 LLM API。

> **`openai` provider 的 Chat/ChatStream 基于 [`sashabaranov/go-openai`](https://github.com/sashabaranov/go-openai) 执行**
> （成熟 HTTP / SSE / 重试 / 错误归一），统一 `dto` <-> go-openai 由本包映射。只有当
> 设置了 `RequestTransform` 或 `Raw`（需要 go-openai 未建模的任意 body 字段）时，才回退到
> 本仓库 `protocol/ghttp` 的序列化 map + 透传路径。

### 双协议支持：Chat Completions / Responses

OpenAI 自身有**两套**互相独立的协议：

- **Chat Completions** (`/chat/completions`)：业界最通用，作为统一基准。
- **Responses API** (`/responses`)：OpenAI 新协议，走 `input` 而非 `messages`。

两者 `dto` 结构不同，**分别建模、互不混用**。Responses 作为**可插拔的增值能力**，
仅当供应商实现对应能力时才生效，是深水区逃生通道，不影响主流 Chat 路径。

### 逃生舱：`ChatRequest.Raw`

`dto.ChatRequest` 提供 `Raw any` 字段（`json:"-"`）。非空时 provider **原样透传**该值
作为请求体，不经过统一协议转换，用于下发统一结构未覆盖的供应商独有字段。

### OpenAI 兼容「小差异」治理：`ModelMapping` + `RequestTransform`

现存的 OpenAI 兼容供应商（deepseek、智谱、kimi、硅基流动等）之间常有**细微协议差异**
（deepseek 的 thinking 字段、kimi 的 temperature 约束、xai 的 search_parameters 等）。
这些差异不该写进传输层（`ghttp`），而是由 `llm` 语义层治理。两种方式都不需要新增 provider 包：

```go
client, _ := llm.NewClient(llm.Config{
    BaseURL: "https://api.deepseek.com/v1",
    APIKey:  "sk-xxx",
    Model:   "deepseek-chat",

    // 1) 模型映射：逻辑名 -> 各厂商真实模型名（链式重定向、自动防循环）
    ModelMapping: map[string]string{"my-reasoner": "deepseek-reasoner"},

    // 2) 请求前钩子：直接改「序列化后的 OpenAI 兼容请求体 map」
    //    用于注入/删除 upstream 独有字段（仅作用于 openai provider 的 Chat/ChatStream）
    RequestTransform: func(req *dto.ChatRequest, body map[string]any) error {
        if req.Model == "deepseek-reasoner" {
            body["thinking"] = map[string]any{"type": "enabled"}
        }
        return nil
    },
})
```

- **默认（不配置 transform / Raw）**：`Chat`/`ChatStream` 完全走 go-openai。
  差异字段只要属于 go-openai 已建模的扩展即可直接用——例如 deepseek 的
  `reasoning_content` / `reasoning_effort`（go-openai 内置支持）。
- **配置了 `RequestTransform`（或 `Raw`）**：表示需要 go-openai 未建模的任意 body 字段。
  此时 openai provider 回退到 `protocol/ghttp`，把统一请求体先序列化为 map、应用该钩子
  （或整体透传 Raw）再发包；`stream=true`、`model` 等字段自然跟随，不影响流式协议。
- `anthropic` / `gemini` 为异协议，不使用这两个配置；对应差异仍走各自的双向映射。

## 目录结构

```
llm/
├── dto/          # 统一请求/响应结构（对齐 OpenAI 协议）
├── provider/
│   ├── openai/   # OpenAI 兼容 provider（基于 go-openai 执行 Chat/ChatStream；含 dto 映射 convert.go）
│   ├── anthropic/# Anthropic Claude Messages provider（异协议，含双向映射）
│   └── gemini/   # Google Gemini provider（异协议，含双向映射）
├── provider.go   # Provider 接口 + 注册表 + 可选接口（transform / go-openai 注入）
├── config.go     # Config（BaseURL/APIKey/Model/HTTP 透传/Responses 开关/ModelMapping/RequestTransform）
└── client.go     # 统一入口 Client（含 go-openai 客户端构造）
```

## 安装与使用

```go
import "github.com/morehao/golib/llm"
import "github.com/morehao/golib/llm/dto"

client, err := llm.NewClient(llm.Config{
    BaseURL: "https://api.deepseek.com/v1", // 兼容供应商改 BaseURL + Model 即覆盖
    APIKey:  "sk-xxx",
    Model:   "deepseek-chat",
})
if err != nil {
    panic(err)
}
```

### 非流式对话

```go
resp, err := client.Chat(ctx, &dto.ChatRequest{
    Messages: []dto.ChatMessage{
        {Role: dto.RoleSystem, Content: "你是一个助手"},
        {Role: dto.RoleUser, Content: "你好"},
    },
})
if err != nil {
    // err 可能为 *ghttp.HTTPError，携带上游状态码与错误信息
    return err
}
fmt.Println(resp.Choices[0].Message.Content)
```

### 流式对话

```go
var sb strings.Builder
err = client.ChatStream(ctx, &dto.ChatRequest{
    Model:    "deepseek-chat",
    Messages: []dto.ChatMessage{{Role: dto.RoleUser, Content: "写首诗"}},
}, func(chunk *dto.StreamChunk) error {
    if len(chunk.Choices) > 0 && chunk.Choices[0].Delta != nil {
        if s, ok := chunk.Choices[0].Delta.Content.(string); ok {
            sb.WriteString(s)
        }
    }
    return nil // 返回错误即终止读取
})
```

### 逃生舱：透传原始请求体

```go
client.Chat(ctx, &dto.ChatRequest{
    Raw: map[string]any{"custom_field": "value", "messages": [...]},
})
```

## 可插拔增值能力

基础 `Provider` 只约定 Chat 能力。**Responses / Embedding / Image / Audio** 作为可插拔增值能力，
通过可选的子接口（`llm.ResponsesProvider` / `llm.EmbeddingProvider` / `llm.ImageProvider` /
`llm.AudioProvider`）按供应商能力选择性实现。`Client.xxx()` 方法内部用类型断言判定当前供应商
是否支持，不支持则返回对应的 `ErrXxxNotSupported`。

### Responses API（OpenAI /responses，走 `input` 而非 `messages`）

```go
resp, err := client.Responses(ctx, &dto.ResponsesRequest{
    Input: []dto.ResponseInputItem{{Role: dto.RoleUser, Content: "hello"}},
})
if err != nil { return err }
fmt.Println(resp.OutputText()) // 拼接后的纯文本

// 流式：逐事件回调
err = client.ResponsesStream(ctx, &dto.ResponsesRequest{Input: "hi"}, func(evt *dto.ResponsesStreamEvent) error {
    if evt.Type == "response.output_text.delta" {
        fmt.Print(evt.Delta)
    }
    return nil
})
```

### Embedding（OpenAI /embeddings）

```go
resp, err := client.Embedding(ctx, &dto.EmbeddingRequest{
    Model: "text-embedding-3-small",
    Input: "hello", // 或 []string{...}
})
// resp.Data[0].Embedding 为 []float64
```

### Image（OpenAI /images/generations）

```go
resp, err := client.Image(ctx, &dto.ImageRequest{
    Model:  "dall-e-3",
    Prompt: "a cat",
    Size:   "1024x1024",
})
// resp.Data[0].URL 或 .B64JSON
```

### Audio（OpenAI /audio/transcriptions，multipart 上传）

```go
resp, err := client.AudioTranscription(ctx, &dto.AudioRequest{
    Model: "whisper-1",
    File:  "/tmp/audio.wav",
})
// resp.Text 为转写文本
```

> `anthropic` / `gemini` 目前未实现上述增值能力（`Client.Embedding` 等会返回对应
> `ErrXxxNotSupported`），基础 `Chat` / `ChatStream` 不受影响。

## 底层 HTTP 复用

复用 golib 自有组件，不重复造轮子：

- `protocol/ghttp` 提供连接池、指数退避重试（可按 `RetryOnStatus` 命中 429/5xx）、
  POST/流式请求。`Config.HTTP` 透传相关配置。
- 流式走 `ghttp.PostStream`，SSE 长连接不会被超时截断。
- 2xx 之外均归一为 `*ghttp.HTTPError`，并从上游 error body 提取可读信息。

## 新增一个供应商

1. `llm/provider/<name>/` 下新建包。
2. 实现 `llm.Provider` 接口，在 `init()` 调用 `llm.RegisterProvider(name, factory)`。
3. OpenAI 兼容供应商直接复用 `openai` provider，无需新包。

### 已支持的供应商

| Provider | 协议 | 说明 |
|----------|------|------|
| `openai` | OpenAI Chat Completions | 默认；通过 BaseURL + 模型名覆盖绝大多数 OpenAI 兼容供应商 |
| `anthropic` | Claude Messages | 异协议，双向映射系统提示词、消息交替、tool_use/tool_result、流式事件 |
| `gemini` | Gemini generateContent | 异协议，映射 contents/parts、systemInstruction、functionCall、流式 |

使用异协议供应商只需改 `ProviderName`：`llm.Config{ProviderName: "anthropic", ...}`。

### 规划路线

- **P0（已完成）**：`openai` 兼容 provider，`Chat` / `ChatStream`，覆盖绝大多数 LLM API。
- **P1（已完成）**：`anthropic`、`gemini` 异协议 provider 的双向字段映射。
- **增值能力（已完成）**：Responses API / Embedding / Image / Audio，全部作为可插拔能力挂在 `openai` provider 上。
