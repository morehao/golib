package gllm

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/cloudwego/eino/components/model"
)

// TestModel 是测试里统一使用的模型名。
const TestModel = "test-model"

// driverSeq 为测试生成不重复的驱动类型名。
// 注册表是进程级全局的，且重复注册会 panic，因此每个用例必须用独立名字。
var driverSeq atomic.Int64

// uniqueDriverType 返回一个本次进程内未使用过的驱动类型名。
func uniqueDriverType() string {
	return fmt.Sprintf("testdriver%d", driverSeq.Add(1))
}

// registerStub 注册一个返回固定 fallback 模型的桩驱动，返回其类型名。
func registerStub(t *testing.T, opts ...DriverOption) string {
	t.Helper()
	name := uniqueDriverType()
	Register(name, func(_ context.Context, _ Resolved) (model.ToolCallingChatModel, error) {
		return newFallbackModel("stub"), nil
	}, opts...)
	return name
}

// registerRecorder 注册一个会记录入参的桩驱动，便于断言 New 的传参正确性。
func registerRecorder(t *testing.T, got *captured) string {
	t.Helper()
	name := uniqueDriverType()
	Register(name, func(_ context.Context, r Resolved) (model.ToolCallingChatModel, error) {
		got.resolved = r
		return newFallbackModel("stub"), nil
	})
	return name
}

// registerFailing 注册一个必定失败的桩驱动。
func registerFailing(t *testing.T, err error) string {
	t.Helper()
	name := uniqueDriverType()
	Register(name, func(_ context.Context, _ Resolved) (model.ToolCallingChatModel, error) {
		return nil, err
	})
	return name
}

// registerNilModel 注册一个返回 (nil, nil) 的桩驱动，用于覆盖防御分支。
func registerNilModel(t *testing.T) string {
	t.Helper()
	name := uniqueDriverType()
	Register(name, func(_ context.Context, _ Resolved) (model.ToolCallingChatModel, error) {
		return nil, nil
	})
	return name
}

// captured 保存桩驱动收到的解析结果。
type captured struct {
	resolved Resolved
}

// configWith 用给定 provider 类型与可选改写拼一份最小可用配置：
// provider "main" + 模型 [TestModel]。
func configWith(providerType string, mutate ...func(*Provider)) Config {
	p := Provider{Type: providerType, APIKey: "sk-test"}
	for _, m := range mutate {
		m(&p)
	}
	return Config{
		Providers: map[string]Provider{"main": p},
		Models:    map[string]ModelConfig{TestModel: {Provider: "main"}},
	}
}

// setModelConfig 覆写 [TestModel] 的模型配置。
func setModelConfig(c Config, mc ModelConfig) Config {
	c.Models[TestModel] = mc
	return c
}

// structFieldTags 汇总结构体的全部字段 tag，用于断言冻结的 schema 字段名。
func structFieldTags(v any) string {
	t := reflect.TypeOf(v)
	var b strings.Builder
	for i := range t.NumField() {
		b.WriteString(string(t.Field(i).Tag))
		b.WriteByte('\n')
	}
	return b.String()
}
