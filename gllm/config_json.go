package gllm

import (
	"encoding/json"
	"fmt"
	"time"
)

// Provider.UnmarshalJSON 让 JSON 配置里的 timeout 同时接受 "60s" 与纳秒整数。
//
// 动机与 config_yaml_test.go 里那条守卫用例相同：接入方照着文档写 timeout，
// 不该因为载体是 JSON 就卡在启动期。yaml.v3 原生两种写法都收，这里把 JSON 拉齐。
//
// 只放宽**解码**：编码仍沿用 time.Duration 的原生形态（纳秒整数），
// 不改变已有 JSON 消费方的预期。
//
// 已知代价：自定义解码内部自己调 json.Unmarshal，因此调用方若开了
// Decoder.DisallowUnknownFields()，该选项在 **Provider 对象内部**不再生效
// （provider 里写错的字段名会被忽略，而不是报错）。其余层级不受影响。
func (p *Provider) UnmarshalJSON(data []byte) error {
	// 别名用于去掉方法集，避免递归调用本方法。
	type providerAlias Provider

	aux := struct {
		// 同名遮蔽：depth 0 的字段优先于别名里 depth 1 的 Timeout，
		// 于是这个字段接住两种写法，其余字段照常走默认解码。
		Timeout json.RawMessage `json:"timeout"`
		*providerAlias
	}{providerAlias: (*providerAlias)(p)}

	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if len(aux.Timeout) == 0 {
		return nil
	}

	d, err := parseJSONDuration(aux.Timeout)
	if err != nil {
		return fmt.Errorf("gllm: provider timeout: %w", err)
	}
	p.Timeout = d
	return nil
}

// parseJSONDuration 解析 timeout：字符串走 time.ParseDuration，数字按纳秒。
func parseJSONDuration(raw json.RawMessage) (time.Duration, error) {
	if string(raw) == "null" {
		// 与「字段缺失」一致：视为未设置，交给 withDefaults 回落。
		return 0, nil
	}

	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return time.ParseDuration(s)
	}

	var n int64
	if err := json.Unmarshal(raw, &n); err != nil {
		return 0, fmt.Errorf(
			`want a duration string like "60s" or an integer nanosecond count, got %s`, raw)
	}
	return time.Duration(n), nil
}
