# gllm

跨项目的 LLM 接入配置层：把「配置」解析为 eino 的 `model.ToolCallingChatModel`。

它解决的问题不是「怎么和某家厂商说话」——那已经由 eino-ext 覆盖；而是
「用哪家、用哪个模型、失败怎么办」。多个 Go 项目接 LLM 时，真正会各写一遍的是后者。

配置只用两个现实概念：**provider**（一套端点连接信息）与 **model**（模型名）。
不引入「档位」之类的中间抽象层，理由见[模型](#模型model)一节。

---

## 设计边界（最重要的一节）

| 做 | 不做 |
|---|---|
| 配置值对象、模型解析、启动期自检 | **不实现任何厂商协议**——协议由 eino-ext 组件承担 |
| 缺凭证时的降级策略与可探测标记 | **不内置重试循环**——只提供 `Retryable` 判定 |
| 错误归类与可重试判定 | **不拼接 BaseURL**——不做隐式 `/v1` 补全 |
| 驱动注册表与能力表 | **不读配置文件**——加载方式由各项目自理 |
| eino 模型的构造 | 不做限流、配额、计费、多租户、审计 |

核只依赖 eino 本体，**不依赖任何 eino-ext 组件**；厂商组件由驱动子包 blank import
引入，使用方按需拉取（可用 `go list -deps ./gllm | grep eino-ext` 验证，结果为空）。

---

## 支持的驱动

| 驱动 | 注册名 | 说明 |
|---|---|---|
| OpenAI 兼容 | `openai` | 见 `gllm/driver/openai`。覆盖 OpenAI 官方端点，以及 DeepSeek、Qwen/DashScope 兼容模式、火山 Ark、本地 vLLM、Ollama 等一切 OpenAI 兼容 API |
| 降级（内置） | `fake` | 随核自动注册，无需 blank import。返回固定回复，不发出任何请求 |

新增驱动只需三件事：实现 `gllm.Factory`、在 `init()` 里 `gllm.Register`、按需声明能力与
是否需要 APIKey。参考 `gllm/driver/openai/driver.go`（约 100 行）。

---

## 快速开始

```go
import (
    "github.com/morehao/golib/gllm"
    _ "github.com/morehao/golib/gllm/driver/openai" // blank import 触发注册
)

cfg := gllm.Config{
    // 别写死 true：Key 环境变量名拼错时会静默返回占位回复
    AllowDegraded: os.Getenv("APP_ENV") != "prod",
    Providers: map[string]gllm.Provider{
        "main": {
            Type:    "openai",
            // 按厂商文档写全：DeepSeek 官方给的就是不带 /v1 的这个
            BaseURL: "https://api.deepseek.com",
            APIKey:  os.Getenv("DEEPSEEK_API_KEY"),
            Timeout: 60 * time.Second,
        },
    },
    Models: map[string]gllm.ModelConfig{
        // key 就是模型名，会原样发给端点
        "deepseek-v4-pro": {
            Provider: "main",
            Extra:    map[string]any{"reasoning_effort": "high"},
        },
        // DeepSeek V4 系列思考模式默认开启，思维链会一起吃掉 max_tokens；
        // 想让采样参数（temperature）真正生效，先用 extra 关掉思考模式。
        "deepseek-flash": {
            Provider:    "main",
            Temperature: ptr[float32](0.2),
            MaxTokens:   ptr(2048),
            Extra:       map[string]any{"thinking": map[string]any{"type": "disabled"}},
        },
    },
}

// 启动期自检：不构造模型、不发请求，配置错误在这里就暴露
if _, err := gllm.Resolve(cfg, "deepseek-v4-pro"); err != nil {
    return err
}

m, err := gllm.New(ctx, cfg, "deepseek-v4-pro")
if err != nil {
    return err
}
if m.Degraded {
    log.Warn("llm 运行在降级模式，不会发出真实请求")
}

resp, err := m.ChatModel.Generate(ctx, []*schema.Message{
    {Role: schema.User, Content: "你好"},
})
```

`m.ChatModel` 是标准 eino 模型，可直接传给 eino 的 `ChatModelAgent` 等组件。

（上面用到的 `ptr` 是项目里常见的泛型小助手，给 `Temperature` / `MaxTokens` 这两个指针字段取址：
`func ptr[T any](v T) *T { return &v }`。）

---

## 配置 schema

字段名与语义是**跨项目冻结契约**，变更走新增字段而非改名。

| 字段 | 类型 | 必填 | 默认 | 语义 |
|---|---|---|---|---|
| `providers` | map | 是 | — | 命名 provider 档案，key 由使用方自取 |
| `providers.*.type` | string | 是 | — | 已注册的驱动名；未注册返回 `ErrProviderUnsupported` |
| `providers.*.base_url` | string | 否 | 驱动默认 | **原样传给驱动**，按厂商文档写全：OpenAI 官方含 `/v1`，DeepSeek 官方不含 |
| `providers.*.api_key` | string | 条件 | — | 是否必填由驱动声明；缺且不允许降级时返回 `ErrAuth` |
| `providers.*.timeout` | duration | 否 | `60s` | 单次请求超时。`"60s"` 与纳秒整数都接受，yaml / json 口径一致。需要「不超时」请给很大的值，因为 0 有默认语义 |
| `providers.*.headers` | map | 否 | — | 透传自定义请求头 |
| `models` | map | 是 | — | **key 就是模型名**，值为该模型的配置 |
| `models.*.provider` | string | 是 | — | 必须存在于 `providers` |
| `models.*.temperature` | *float32 | 否 | 驱动默认 | 指针以区分「未设置」与「设为 0」 |
| `models.*.max_tokens` | *int | 否 | 驱动默认 | 同上。映射到 `max_tokens`，不是 `max_completion_tokens`，见下 |
| `models.*.extra` | map | 否 | — | 驱动私有逃生通道，核不解释；驱动原样并入请求体 |
| `allow_degraded` | bool | 否 | `false` | 缺 APIKey 时是否退化为 fake |

对应的 yaml：

```yaml
llm:
  providers:
    main:
      type: openai
      base_url: https://api.deepseek.com   # 按厂商文档写全，gllm 不做任何拼接
      api_key: ${DEEPSEEK_API_KEY}
      timeout: 60s
    thinking:                              # 连接参数只在 provider 级：
      type: openai                         # 要给思考模型更长超时，就得拆一个 provider
      base_url: https://api.deepseek.com
      api_key: ${DEEPSEEK_API_KEY}
      timeout: 300s
  models:
    deepseek-flash:
      provider: main
      temperature: 0.2
      max_tokens: 2048
      extra: {thinking: {type: disabled}}  # 关掉思考模式，上面的采样参数才生效
    deepseek-v4-pro:
      provider: thinking
      extra: {reasoning_effort: high}
  allow_degraded: true
```

同样的内容用 json（`timeout` 写成 `"60s"` 或纳秒整数均可）：

```json
{
  "allow_degraded": true,
  "providers": {
    "main": {
      "type": "openai",
      "base_url": "https://api.deepseek.com",
      "api_key": "sk-...",
      "timeout": "60s"
    }
  },
  "models": {
    "deepseek-flash": {"provider": "main", "temperature": 0.2, "max_tokens": 2048}
  }
}
```

---

## 模型（model）

`models` 段的 **key 就是模型名本身**，调用点直接写这个名字，不需要在两层名字之间做映射：

```go
m, err := gllm.New(ctx, cfg, "deepseek-flash")
```

建议把模型名定义成项目侧常量，避免裸字符串散落各处：

```go
const (
    ModelPlanner = "deepseek-v4-pro" // 规划、复核等「错了代价高」的步骤
    ModelGeneric = "deepseek-flash"  // 分类、抽取、摘要等高频步骤
)
```

**「key 即模型名」的代价**：上游改名或下线时这份映射会失效，因为 gllm 不做别名转换、
key 会被原样发给端点。看 DeepSeek 的节奏就知道这有多频繁：`deepseek-chat` /
`deepseek-reasoner` 于 2026-04-24 公告停用、2026-07-24 起不再可用
（[更新日志](https://api-docs.deepseek.com/zh-cn/updates)），现役只有 `deepseek-flash`
与 `deepseek-v4-pro`。名字过期属于 `ErrBadRequest`，**既不降级也不可重试**，
所以别把模型名当稳定契约——收进项目侧常量，改名时至少只改一处。

**为什么不内置「档位」（如 `strong` / `cheap`）。** 档位这类间接层能让你
「换模型只改配置、不改代码」，但它要求在 provider 与 model 之外再引入一个现实中不存在的概念，
每个接入方都得先学一遍它的语义；而这层间接真正省事的场景很窄——只有
「同一个逻辑用途要在多个环境映射到不同模型」时才划算，用项目侧常量配合按环境替换配置文件
同样能解决。gllm 选择不替你决定这个映射：需要时在项目里加一个
`func modelFor(purpose string) string` 即可，语义由你定，不必让 gllm 承担。

---

## 降级语义

**只有「缺凭证」会降级**，配置错误与运行期错误都不降级。

| 情况 | `allow_degraded=false` | `allow_degraded=true` |
|---|---|---|
| `type` 未注册 | `ErrProviderUnsupported` | 同左 |
| 结构非法 / model 或 provider 不存在 | `ErrConfigInvalid` | 同左 |
| `api_key` 为空（驱动要求 Key） | `ErrAuth` | 返回 fake，`Degraded=true`，打 `Warn` |
| `type == "fake"` | 返回 fake，`Degraded=true` | 同左 |
| 驱动构造报错 / 运行期调用失败 | 归一化错误，不降级 | 同左 |

默认 `allow_degraded=false`：静默降级会掩盖配置错误。

`Model.Degraded` 让调用方**能断言**——单测可断言「本地无 Key 时拿到降级模型」，
生产可据此打点告警。生产环境出现 `Degraded` 通常意味着漏配了 Key。

驱动可声明 `WithNoAuth()` 表示不需要 Key（本地 vLLM、自建网关等），此时缺 Key 不触发降级。

---

## 错误分类与重试

错误码段 `120000-120099`，定义在 `gconstant/error_code.go`。

| 错误码 | 哨兵 | 可重试 | 场景 |
|---|---|---|---|
| 120000 | `ErrConfigInvalid` | 否 | 配置非法 |
| 120001 | `ErrProviderUnsupported` | 否 | 驱动未注册（漏了 blank import） |
| 120002 | `ErrAuth` | 否 | 401 / 403，或缺 Key |
| 120003 | `ErrRateLimit` | **是** | 429、配额耗尽 |
| 120004 | `ErrTimeout` | **是** | 超时、context deadline |
| 120005 | `ErrUpstream` | **是** | 上游 5xx，及无法归类的其他错误 |
| 120006 | `ErrBadRequest` | 否 | 400 / 404 / 422 |
| 120007 | `ErrContentFilter` | 否 | 内容过滤拦截 |
| 120008 | `ErrDegraded` | 否 | 已降级（可探测，非致命） |

`Classify` 的判定顺序是**先结构化后文本**：`context.DeadlineExceeded` → HTTP 状态码
（`StatusCode()` / `HTTPStatusCode()` / 文本中的 `status: 429` 等）→ 关键字兜底 →
落到 `ErrUpstream`（保守取可重试一侧）。

```go
if err := doSomething(); err != nil {
    err = gllm.Classify(err)
    if gllm.Retryable(err) {
        // 按 gllm.RetryMaxAttempts / RetryBaseDelay / RetryMaxDelay 自行退避
    }
    if errors.Is(err, gllm.ErrAuth) {
        // 告警：Key 配置有问题
    }
}
```

**重试为什么不在库内**：重试与调用方的超时预算、并发控制、幂等语义耦合，
必须由调用方或 eino callback 决定。`gllm` 只给出统一的退避参数建议值：
`RetryMaxAttempts = 3`、`RetryBaseDelay = 1s`、`RetryMaxDelay = 30s`。

---

## 能力表与其已知局限

能力（Tools / Vision / Reasoning）按**驱动类型**登记，可用 `gllm.Capabilities(driverType)`
与 `gllm.Supports(driverType, want)` 查询；`Model.ProviderType` 可直接用于查询。

**已知局限**：能力是模型级属性，却登记在驱动类型上——同一个 `openai` 驱动下，
`deepseek-flash` 支持图像理解而 `deepseek-v4-pro` 不支持，能力表无法区分。因此能力表只应被用来
挡住「这套驱动根本不支持工具」这类错误，不能用来判断同一驱动下某个具体模型是否支持某能力。
`openai` 驱动因此**故意不声明** `Reasoning`。

---

## 两条容易踩的边界

1. **BaseURL 按厂商文档写全，gllm 与驱动都不做任何拼接**。OpenAI 官方是
   `https://api.openai.com/v1`；DeepSeek 官方给的是不带 `/v1` 的
   `https://api.deepseek.com`；不少第三方 OpenAI 兼容网关（如 `llm.yygu.cn`）
   则必须写到 `/v1`。少一段的典型症状是 404 / `path not found`——拿不准就用接入指南
   §4 的启动期探活一次钉死。
2. **`max_tokens` 按字面映射到 `max_tokens`，不是 `max_completion_tokens`**。
   这条是实测结论而非偏好：多数 OpenAI 兼容第三方端点会**静默忽略**
   `max_completion_tokens`——不报错，只让长度限制失效，比 deprecated 更危险。
   需要 `max_completion_tokens` 的模型（OpenAI o1/o3 系列）通过 `models.*.extra` 覆盖：

   ```yaml
   providers:
     official:
       type: openai
       base_url: https://api.openai.com/v1
   models:
     o3:
       provider: official
       extra: {max_completion_tokens: 4096}
   ```

   `extra` 会被驱动原样并入请求体，同名键覆盖基础字段。

---

## 测试与依赖边界

```bash
go test -race ./gllm/...
go list -deps ./gllm | grep -c eino-ext   # 期望 0
go list -deps ./gllm/driver/openai | grep -c eino-ext  # 期望 > 0
```

---

## 相关文档

| 文档 | 位置 | 内容 |
|---|---|---|
| **接入指南** | `docs/gllm-integration-guide.md` | 调用方如何把 gllm 接进业务项目：分步接入、必须做的决策、排查表、Checklist |
| 驱动参考 | `gllm/driver/openai/README.md` | OpenAI 兼容端点的字段映射与实测结论 |

本文档是**参考手册**（字段、语义、错误码）；接入步骤与工程决策见接入指南。
