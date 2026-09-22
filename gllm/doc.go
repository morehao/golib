// Package gllm 提供跨项目的 LLM 接入配置层：把「配置」解析为 eino 的
// [model.ToolCallingChatModel]。
//
// 配置只用两个现实概念：Provider（一套端点连接信息）与模型名。不引入「档位」之类的
// 中间抽象层——调用方要哪个模型就写哪个模型名，映射关系不由 gllm 替你决定。
//
// 设计边界（重要，改动前请先确认没有越过这些线，接入方式见 docs/gllm-integration-guide.md）：
//
//   - gllm 不实现任何厂商协议。协议由 eino-ext 组件承担，驱动子包只做接线与注册。
//   - gllm 核只依赖 eino 本体，不依赖任何 eino-ext 组件；厂商组件由驱动子包
//     blank import 引入，使用方按需拉取。
//   - gllm 不内置重试循环，只提供 [Retryable] 判定。重试与调用方的超时预算、
//     并发控制、幂等语义耦合，交由调用方或 eino callback 决定。
//   - gllm 不拼接 BaseURL。端点路径由使用方按厂商文档写全（OpenAI 官方含 `/v1`，
//     DeepSeek 官方不含）。
//
// 典型用法：
//
//	import (
//	    "github.com/morehao/golib/gllm"
//	    _ "github.com/morehao/golib/gllm/driver/openai" // 注册 "openai" 类型
//	)
//
//	// 要哪个模型就写哪个模型名，即 Config.Models 的 key
//	m, err := gllm.New(ctx, cfg, "deepseek-flash")
//	if err != nil {
//	    return err
//	}
//	if m.Degraded {
//	    log.Warn("llm 运行在降级模式，不会发出真实请求")
//	}
//	resp, err := m.ChatModel.Generate(ctx, messages)
package gllm

// Version 是 gllm 的语义化版本，随包内契约（Config schema / 错误码 / 导出签名）
// 的兼容性变化而更新。
const Version = "v0.1.0"
