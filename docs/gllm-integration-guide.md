# gllm 接入指南

> 适用对象：要在业务项目里接入 LLM 的调用方
> 前置：已 `go get github.com/morehao/golib`（版本包含 `gllm` 包）
> 参考：包内参考手册 `gllm/README.md`（字段、语义、错误码）

## 一、顶层结论

`gllm` 只解决四件事：**配置解析、模型构造、降级兜底、错误归一**。

它**不**做三件事，这三件事必须由你的项目承担：

| gllm 不做 | 由谁做 |
|---|---|
| 读配置文件 | 你（`gllm.Config` 是纯值对象，怎么加载由项目决定） |
| 重试 | 你（`gllm` 只给 `Retryable` 判定与退避参数建议） |
| 实现厂商协议 | eino-ext 组件（驱动只做接线） |

所以「接入 gllm」的本质是：**把这四件事接到你项目已有的配置加载、日志、重试体系上**。
净新增胶水代码约 60 行。

设计上刻意只用两个现实概念——`provider`（怎么连）与 `model`（用哪个）——
不引入「档位」之类的中间抽象层。理由见 §4.2。

---

## 二、最小可运行

```go
package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/cloudwego/eino/schema"

	"github.com/morehao/golib/gllm"
	_ "github.com/morehao/golib/gllm/driver/openai" // blank import 触发驱动注册
)

func main() {
	cfg := gllm.Config{
		AllowDegraded: true, // 本地无 Key 时退化为 fake，而不是启动失败
		Providers: map[string]gllm.Provider{
			"main": {
				Type:    "openai",
				BaseURL: os.Getenv("LLM_BASE_URL"), // 按厂商文档写全，gllm 不拼接
				APIKey:  os.Getenv("LLM_API_KEY"),
				Timeout: 60 * time.Second,
			},
		},
		Models: map[string]gllm.ModelConfig{
			// key 就是模型名，会原样发给端点
			"deepseek-v4-pro": {Provider: "main"},
			// DeepSeek V4 系列思考模式默认开启，思维链会一起吃掉 max_tokens；
			// 想让采样参数真正生效，先用 extra 关掉思考（见 §7 排查表）
			"deepseek-flash": {
				Provider:    "main",
				Temperature: ptr[float32](0.2),
				MaxTokens:   ptr(2048),
				Extra:       map[string]any{"thinking": map[string]any{"type": "disabled"}},
			},
		},
	}

	m, err := gllm.New(context.Background(), cfg, "deepseek-v4-pro")
	if err != nil {
		log.Fatalf("gllm: %v", err)
	}
	if m.Degraded {
		log.Print("gllm: 降级模式，不会发出真实请求")
	}

	resp, err := m.ChatModel.Generate(context.Background(), []*schema.Message{
		{Role: schema.User, Content: "你好"},
	})
	if err != nil {
		log.Fatalf("gllm: %v", gllm.Classify(err))
	}
	log.Println(resp.Content)
}

func ptr[T any](v T) *T { return &v }
```

跑通这段，接入就完成了一半。剩下的是把它接到你的工程结构里。

---

## 三、接入步骤

### 1. 引入依赖与驱动

```go
import (
	"github.com/morehao/golib/gllm"
	_ "github.com/morehao/golib/gllm/driver/openai"
)
```

驱动的 blank import **放在哪里很重要**：只放在 `main` 包里，
那么 `gllm.New` 在测试或其他入口被调用时驱动是未注册的。
建议放在你对 gllm 做封装的那个包（见步骤 4）里，保证任何调用路径都先经过它。

漏了 blank import 的症状是 `ErrProviderUnsupported`，且错误信息里会列出所有已注册类型。

### 2. 定义项目侧配置

`gllm.Config` 已带 json/yaml tag，可直接作为你配置结构的一部分嵌入：

```go
type AppConfig struct {
	Server struct{ Addr string `yaml:"addr"` } `yaml:"server"`
	LLM    gllm.Config                        `yaml:"llm"` // 直接嵌入
}
```

对应的 YAML：

```yaml
llm:
  allow_degraded: false
  providers:
    main:
      type: openai
      base_url: https://llm.yygu.cn/v1
      api_key: ${LLM_API_KEY}      # 由加载层做环境变量展开
      timeout: 60s
  models:
    deepseek-v4-pro: {provider: main}
    deepseek-flash:  {provider: main, max_tokens: 2048}
```

`models` 的 **key 就是模型名**，没有再套一层。要哪个模型就在调用点写哪个名字。

**推荐做法**：把 `api_key` 留空写在 YAML 里，加载后从环境变量/密钥管理注入。
密钥不进配置文件、不进版本库。

### 3. 加载配置（gllm 不做这件事）

golib 已依赖 `gopkg.in/yaml.v3`，直接用即可：

```go
func LoadConfig(path string) (*AppConfig, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	// 展开 ${VAR}，让 base_url / api_key 也能按环境区分
	expanded := os.ExpandEnv(string(raw))

	var cfg AppConfig
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, err
	}
	if key := os.Getenv("LLM_API_KEY"); key != "" {
		// 逐个 provider 注入，避免密钥落盘
		for name, p := range cfg.LLM.Providers {
			if p.APIKey == "" {
				p.APIKey = key
				cfg.LLM.Providers[name] = p
			}
		}
	}
	return &cfg, nil
}
```

纯环境变量也可以，不引 yaml：

```go
cfg := gllm.Config{
	Providers: map[string]gllm.Provider{
		"main": {Type: "openai", BaseURL: os.Getenv("LLM_BASE_URL"), APIKey: os.Getenv("LLM_API_KEY")},
	},
	Models: map[string]gllm.ModelConfig{
		"deepseek-v4-pro": {Provider: "main"},
		"deepseek-flash":  {Provider: "main"},
	},
}
```

### 4. 启动期自检与降级探测

**在 `main` 里、在开始接收流量之前**完成，让配置错误在部署阶段暴露而不是在第一次用户请求时暴露：

```go
// 项目侧声明的模型名常量，见 §4.2
const (
	ModelPlanner = "deepseek-v4-pro"
	ModelGeneric = "deepseek-flash"
)

func SetupLLM(ctx context.Context, cfg gllm.Config, isProd bool) (*gllm.Model, error) {
	// ① 校验每个会用到的模型：纯解析、无 I/O，能查出引用错误、拼写错误、未注册驱动
	for _, name := range []string{ModelPlanner, ModelGeneric} {
		if _, err := gllm.Resolve(cfg, name); err != nil {
			return nil, fmt.Errorf("llm 配置 %s 非法: %w", name, err)
		}
	}

	// ② 构造默认模型
	m, err := gllm.New(ctx, cfg, ModelGeneric)
	if err != nil {
		return nil, fmt.Errorf("llm 构造失败: %w", err)
	}

	// ③ 降级探测：按环境决定是告警还是拒绝启动
	if m.Degraded {
		if isProd {
			// 生产环境静默降级 = 用户拿到假回复，必须挡住
			return nil, errors.New("llm 生产环境不允许降级运行，请检查 API Key 配置")
		}
		glog.Warnw(ctx, "llm 运行在降级模式，返回的是占位回复",
			"model", m.ModelName, "provider", m.ProviderName, "version", gllm.Version)
	}

	glog.Infow(ctx, "llm 就绪", "version", gllm.Version,
		"model", m.ModelName, "provider", m.ProviderName,
		"degraded", m.Degraded)
	return m, nil
}
```

`gllm.Version` 请务必打出来：排查线上问题时，「这个服务跑的是哪版 gllm 契约」是第一手信息。

**可选但推荐**：`New` 只构造对象、**不发起网络请求**（已核对 eino-ext 实现），
所以 Key 无效、端点不可达这类问题要等到第一次真实调用才暴露。
若你的服务不能容忍「启动成功但 LLM 全挂」，在启动期补一次极短的探活调用：

```go
probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
defer cancel()
if _, err := m.ChatModel.Generate(probeCtx, []*schema.Message{
	{Role: schema.User, Content: "ping"},
}); err != nil {
	return nil, fmt.Errorf("llm 探活失败: %w", gllm.Classify(err))
}
```

### 5. 在业务代码里使用

**构造一次，长期复用**，不要每次请求都 `gllm.New`：

```go
type ChatService struct {
	llmPlanner *gllm.Model // 规划、复核
	llmGeneric *gllm.Model // 其余
}

func NewChatService(ctx context.Context, cfg gllm.Config) (*ChatService, error) {
	planner, err := gllm.New(ctx, cfg, ModelPlanner)
	if err != nil {
		return nil, err
	}
	generic, err := gllm.New(ctx, cfg, ModelGeneric)
	if err != nil {
		return nil, err
	}
	return &ChatService{llmPlanner: planner, llmGeneric: generic}, nil
}

func (s *ChatService) Summarize(ctx context.Context, text string) (string, error) {
	resp, err := s.llmGeneric.ChatModel.Generate(ctx, []*schema.Message{
		{Role: schema.System, Content: "你是摘要助手。"},
		{Role: schema.User, Content: text},
	})
	if err != nil {
		return "", gllm.Classify(err)
	}
	return resp.Content, nil
}
```

**为什么必须复用**：openai 驱动在构造时创建 `*http.Client`（内含连接池）。
每次 `New` 都新建 client，等于每个请求重做 TCP + TLS 握手，长连接复用完全失效。

### 6. 与 eino 编排组件组合

`m.ChatModel` 是标准 eino 模型，可直接交给 eino 的编排组件：

```go
agent, err := react.NewAgent(ctx, &react.AgentConfig{
	ToolCallingModel: m.ChatModel, // 需要 Tools 能力，见下
	ToolsConfig:      toolsConfig,
})
```

用工具前先查能力，避免用不支持的模型去绑工具：

```go
if !gllm.Supports(m.ProviderType, gllm.Capability{Tools: true}) {
	return fmt.Errorf("驱动 %s 未声明 Tools 能力", m.ProviderType)
}
```

能力表的粒度是**驱动类型**而非模型（已知局限，见 `gllm/driver/openai/README.md`），
所以它只能用来挡住「这套驱动根本不支持工具」这类错误，不能用来区分同一驱动下的不同模型。

若需要带工具的模型实例，用 `WithTools`（返回**新实例**，不改原对象）：

```go
withTools, err := m.ChatModel.WithTools(toolInfos)
if err != nil {
	return err
}
// 同样建议构造一次后缓存，不要每次调用都 WithTools
```

### 7. 错误处理与重试

统一先 `Classify`，再按 `Retryable` 决定：

```go
func generateWithRetry(ctx context.Context, cm model.BaseChatModel, msgs []*schema.Message) (*schema.Message, error) {
	delay := gllm.RetryBaseDelay
	var lastErr error

	for attempt := 1; attempt <= gllm.RetryMaxAttempts; attempt++ {
		resp, err := cm.Generate(ctx, msgs)
		if err == nil {
			return resp, nil
		}

		lastErr = gllm.Classify(err)
		if !gllm.Retryable(lastErr) {
			return nil, lastErr // 鉴权、参数、内容过滤等重试无意义
		}
		if attempt == gllm.RetryMaxAttempts {
			break
		}

		select {
		case <-ctx.Done():
			return nil, gllm.Classify(ctx.Err())
		case <-time.After(delay):
		}
		delay = min(delay*2, gllm.RetryMaxDelay)
	}
	return nil, lastErr
}
```

不要对 `ErrAuth` / `ErrBadRequest` / `ErrContentFilter` 重试——只会浪费配额。
只有 `ErrRateLimit` / `ErrTimeout` / `ErrUpstream` 值得重试。

按错误类型分流处理：

```go
switch {
case errors.Is(err, gllm.ErrAuth):
	alert("LLM Key 配置有问题")            // 运维问题，必须告警
case errors.Is(err, gllm.ErrRateLimit):
	metrics.Incr("llm.ratelimit")          // 容量问题，考虑扩容或换模型
case errors.Is(err, gllm.ErrContentFilter):
	return fallbackReply, nil              // 业务问题，走兜底话术
}
```

---

## 四、几个必须做的决策

### 4.1 `Model` 复用还是每次 `New`

**复用**。启动期构造一次，挂在 service 结构体上（见步骤 5）。
每次新建会丢掉 HTTP 连接池，代价是每请求一次 TLS 握手。

### 4.2 调用点怎么引用模型

`models` 段以**模型名本身**为 key，调用点直接写模型名：

```go
m, err := gllm.New(ctx, cfg, "deepseek-flash")
```

裸字符串散落在调用点上容易写错、换模型时要全局替换。**建议在你的项目里定义常量**：

```go
// 与配置 models 段的 key 一一对应
const (
	ModelPlanner = "deepseek-v4-pro" // 规划、复核等「错了代价高」的步骤
	ModelGeneric = "deepseek-flash"  // 分类、抽取、摘要等高频步骤
)
```

这样至少有编译期引用检查，换模型时改一处常量。常量名带业务语义（planner / generic），
比 `strong` / `cheap` 这类人造档位名更贴近你实际的用途划分。

**为什么 gllm 不内置「档位」这类间接层。** 那种设计能让你「换模型只改配置、不改代码」，
但它要求在 provider 与 model 之外再引入一个现实中不存在的概念，每个接入方都得先学一遍它的语义。
而这层间接真正省事的场景很窄——只有「同一个逻辑用途要在多个环境/多个项目映射到不同模型」时才划算，
用项目自己的常量 + 按环境替换配置文件同样能解决。gllm 选择不替你做这个决定。

### 4.3 超时怎么设

`providers.*.timeout` 默认 60s。注意 **0 有默认语义**，
想要「不超时」必须给一个很大的值，而不是留空。

超时是 **provider 级**（一套端点一个连接配置），所以「快模型给短超时、慢模型给长超时」
要拆成两个 provider。注意拆分会把 `base_url` / `api_key` 一起复制一份——换 Key 时两处都要改：

```yaml
providers:
  fast: {type: openai, base_url: https://api.deepseek.com, timeout: 30s}
  deep: {type: openai, base_url: https://api.deepseek.com, timeout: 300s}
models:
  deepseek-flash:  {provider: fast, extra: {thinking: {type: disabled}}} # 关思考才谈得上「快」
  deepseek-v4-pro: {provider: deep}                                     # 思考模式：给足超时
```

DeepSeek V4 系列**思考模式默认开启**（`reasoning_effort` 默认 `high`），首 token 与
总耗时都明显变长，60s 常常不够；想让 `deepseek-flash` 当「快模型」用，得显式关掉思考。

### 4.4 配置能否热更新

**不支持，且刻意如此**。`gllm.Config` 是不可变值对象，改配置需要重新 `gllm.New`。
若要热更新，在你的项目层实现：收到配置变更 → `New` 出新 `Model` → 原子替换 service 里的指针。

不建议为此在 gllm 里加全局可变状态——多租户场景下会立刻失控。

---

## 五、可观测性

必须采集的四项：

| 指标 | 来源 | 为什么重要 |
|---|---|---|
| `degraded` | `Model.Degraded` | 为 true 说明**根本没在调用真实模型**，用户拿到的是占位回复 |
| 错误分类 | `gllm.Classify(err)` | 区分「Key 错了」（运维）和「限流了」（容量）和「被内容过滤」（业务） |
| `version` | `gllm.Version` | 排查时确认服务跑的是哪版 gllm 契约 |
| provider / 模型 | `Model.ProviderName` / `ProviderType` / `ModelName` | 确认实际生效的是哪套配置 |

`Degraded` 在**生产环境**出现，基本等同于事故——它意味着漏配了 Key。
启动时挡住（步骤 4），运行时也建议打点。

---

## 六、测试接入方代码（不打真实网络）

用内置的 `fake` 驱动，你的服务测试完全不依赖网络和 Key：

```go
cfg := gllm.Config{
	Providers: map[string]gllm.Provider{
		"test": {Type: "fake"},
	},
	Models: map[string]gllm.ModelConfig{
		"my-model": {
			Provider: "test",
			Extra:    map[string]any{"fake_reply": `{"summary":"测试摘要"}`},
		},
	},
}

m, err := gllm.New(context.Background(), cfg, "my-model")
// m.Degraded == true，Generate 返回 Extra 里指定的内容
```

`fake` 驱动随核自动注册，无需 blank import。它**不发起任何网络请求**，可安全用于 CI。

若要跑真实模型集成测试，参考 `gllm/driver/openai/integration_test.go`：
缺配置时 `t.Skip`，需要强制验证时设 `GLLM_OPENAI_REQUIRE=1` 硬失败。
这种「默认跳过 + 可强制」的写法能避免集成用例长期静默失效。

---

## 七、常见错误排查

| 现象 | 原因 | 处理 |
|---|---|---|
| 404 / `path not found` | `base_url` 与厂商文档不一致（少了版本段，或多了路径） | 按厂商文档写全：OpenAI 官方含 `/v1`，DeepSeek 官方不含；gllm 与驱动都不做拼接 |
| `ErrProviderUnsupported` | 漏了驱动的 blank import | 加 `import _ ".../gllm/driver/openai"`，错误信息里会列出已注册类型 |
| `ErrAuth`（401） | Key 错误或缺失 | 检查 `provider.api_key` 是否真的注入了（环境变量名写错很常见） |
| `ErrConfigInvalid`：model not defined | 调用点写的模型名不在 `models` 段里 | 名字必须与 `models` 的 key 完全一致，注意大小写 |
| `ErrBadRequest`：unknown model | 上游模型已改名或下线（DeepSeek 的 `deepseek-chat` / `deepseek-reasoner` 已于 2026-07-24 停用） | 换成厂商文档里的现役名；gllm 不做别名转换，这种错**不降级也不可重试** |
| 请求成功但 `content` 为空 | 思考模式默认开启，思维链把 `max_tokens` 预算吃光 | 调大 `max_tokens`；或读 `resp.ReasoningContent`；不需要思考就用 `extra` 关掉 |
| `temperature` 设了不生效 | DeepSeek 思考模式下不支持 `temperature`，传了不报错也不生效 | 用 `extra` 关思考：`{thinking: {type: disabled}}` |
| 设了 `max_tokens` 却不生效 | 极少数端点忽略该字段 | 用 `models.*.extra` 试 `max_completion_tokens`；先用 `finish_reason` 确认是否真被截断 |
| 生产环境回复是占位文本 | `allow_degraded: true` 且缺 Key | 生产设 `allow_degraded: false`；或检查 Key 注入 |
| 启动就报 `ErrConfigInvalid` | 模型引用了不存在的 provider，或 provider 缺 `type` | 错误信息带具体字段，按提示修 |

`Resolve` 是排查配置问题的第一工具：它纯解析、无 I/O，可以单独调用快速定位。

---

## 八、从手写适配层迁移

如果你现在维护着类似 WeKnora `internal/models/chat` 或 ragflow-Go `internal/entity/models`
那样的手写适配层，迁移顺序建议：

1. **先只迁配置与构造**：把手写的「读配置 → switch 厂商 → 构造客户端」换成
   `gllm.Config` + `gllm.New`，保留你自己的调用封装与重试逻辑不动。
2. **再加错误归一**：把散落各处的 `if strings.Contains(err.Error(), "429")` 换成 `gllm.Classify`。
3. **最后才考虑删驱动**：只有当某个厂商在 **≥2 个项目**里都需要、且 eino-ext 覆盖不到时，
   才把它下沉成 gllm 驱动。否则留在项目内。

**不要一开始就追求全量替换**。手写层里往往混着项目特有的东西（限流、计费、多租户、
SSRF 防护），这些**不属于** gllm，硬塞进来会让 gllm 变成第二个手写层。

如果你原来的手写层用了「档位」这类概念，映射过来就是：把档位名换成它实际指向的模型名。
若确实需要保留一层间接，在你项目里做一个 `func modelFor(purpose string) string` 即可——
放在项目侧，语义由你定，不用让 gllm 承担。

---

## 九、接入 Checklist

- [ ] `go get` golib，确认版本包含 `gllm`
- [ ] blank import 驱动，且放在所有调用路径都会经过的包
- [ ] `gllm.Config` 嵌入项目配置结构，密钥走环境变量/密钥管理而非落盘
- [ ] 确认 `base_url` 与厂商文档一致（gllm 与驱动都不做任何拼接）
- [ ] `models` 段的 key 就是真实模型名，且与项目侧常量一一对应
- [ ] 启动期对**每个会用到的模型**调 `Resolve` 做自检
- [ ] 启动期 `New` 一次并复用，不要在请求路径里构造
- [ ] 生产环境对 `Degraded=true` 拒绝启动
- [ ] 启动日志打印 `gllm.Version` / model / provider / degraded
- [ ] 错误处理统一 `Classify`，只对 `Retryable` 的重试
- [ ] 用 `fake` 驱动写服务单测，CI 不依赖网络
- [ ] （可选）按项目约定补真实模型集成测试，默认跳过 + 可强制失败

---

## 附：相关文档

| 文档 | 位置 | 内容 |
|---|---|---|
| 包内参考 | `gllm/README.md` | 配置 schema 全量字段表、降级语义矩阵、错误码表 |
| 驱动参考 | `gllm/driver/openai/README.md` | OpenAI 兼容端点的字段映射与实测结论 |
