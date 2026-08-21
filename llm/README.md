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

### 双协议支持：Chat Completions / Responses

OpenAI 自身有**两套**互相独立的协议：

- **Chat Completions** (`/chat/completions`)：业界最通用，作为统一基准。
- **Responses API** (`/responses`)：OpenAI 新协议，走 `input` 而非 `messages`。

两者 `dto` 结构不同，**分别建模、互不混用**。Responses 作为**可插拔的增值能力**，
仅当供应商实现对应能力时才生效，是深水区逃生通道，不影响主流 Chat 路径。

### 逃生舱：`ChatRequest.Raw`

`dto.ChatRequest` 提供 `Raw any` 字段（`json:"-"`）。非空时 provider **原样透传**该值
作为请求体，不经过统一协议转换，用于下发统一结构未覆盖的供应商独有字段。

## 目录结构

```
llm/
├── dto/          # 统一请求/响应结构（对齐 OpenAI 协议）
├── provider/
│   ├── openai/   # OpenAI 兼容 provider（Chat/ChatStream）
│   ├── anthropic/# Anthropic Claude Messages provider（异协议，含双向映射）
│   └── gemini/   # Google Gemini provider（异协议，含双向映射）
├── provider.go   # Provider 接口 + 注册表
├── config.go     # Config（BaseURL/APIKey/Model/HTTP 透传/Responses 开关）
└── client.go     # 统一入口 Client
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
- **增值能力**：Responses API / Embedding / Image / Audio。
