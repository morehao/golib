package dbes

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	jsoniter "github.com/json-iterator/go"
)

type Map = map[string]any

// Builder ES DSL构造器
type Builder struct {
	body Map
}

// NewBuilder 创建新的DSL构造器
func NewBuilder() *Builder {
	return &Builder{
		body: make(map[string]any),
	}
}

// Set 通用设置方法，支持链式调用
func (b *Builder) Set(key string, value any) *Builder {
	b.body[key] = value
	return b
}

// SetQuery 设置查询条件
func (b *Builder) SetQuery(query any) *Builder {
	return b.Set("query", query)
}

// SetAggs 设置聚合
func (b *Builder) SetAggs(aggs any) *Builder {
	return b.Set("aggs", aggs)
}

// SetSort 设置排序
func (b *Builder) SetSort(sort any) *Builder {
	return b.Set("sort", sort)
}

// SetSize 设置返回数量
func (b *Builder) SetSize(size int) *Builder {
	return b.Set("size", size)
}

// SetFrom 设置偏移量
func (b *Builder) SetFrom(from int) *Builder {
	return b.Set("from", from)
}

// SetSource 设置返回字段
func (b *Builder) SetSource(fields []string) *Builder {
	return b.Set("_source", fields)
}

// SetHighlight 设置高亮
func (b *Builder) SetHighlight(highlight any) *Builder {
	return b.Set("highlight", highlight)
}

// Build 构建DSL
func (b *Builder) Build() map[string]any {
	return b.body
}

// BuildBytes 构建并返回 []byte
func (b *Builder) BuildBytes() ([]byte, error) {
	return jsoniter.Marshal(b.body)
}

func (b *Builder) BuildReader() (io.Reader, error) {
	if len(b.body) == 0 {
		return strings.NewReader("{}"), nil
	}

	bodyBytes, err := jsoniter.Marshal(b.body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal body to JSON: %w", err)
	}
	return bytes.NewReader(bodyBytes), nil
}

func BuildSortField(field string, order string) Map {
	return BuildMap(field, BuildMap("order", order))
}

func BuildSortScore(order string) Map {
	return BuildMap("_score", BuildMap("order", order))
}

type HighlightCfg struct {
	fragmentSize      int
	numberOfFragments int
	PreTags           []string
	PostTags          []string
}

type HighlightOption interface {
	apply(*HighlightCfg)
}

type funcHighlightOption func(*HighlightCfg)

func (f funcHighlightOption) apply(cfg *HighlightCfg) {
	f(cfg)
}

func WithFragmentSize(size int) HighlightOption {
	return funcHighlightOption(func(cfg *HighlightCfg) {
		cfg.fragmentSize = size
	})
}

func WithNumberOfFragments(number int) HighlightOption {
	return funcHighlightOption(func(cfg *HighlightCfg) {
		cfg.numberOfFragments = number
	})
}

func WithPreTags(tags []string) HighlightOption {
	return funcHighlightOption(func(cfg *HighlightCfg) {
		cfg.PreTags = tags
	})
}

func WithPostTags(tags []string) HighlightOption {
	return funcHighlightOption(func(cfg *HighlightCfg) {
		cfg.PostTags = tags
	})
}

func BuildHighlightField(fields []string, options ...HighlightOption) Map {
	cfg := &HighlightCfg{
		fragmentSize:      1500,
		numberOfFragments: 5,
		PreTags:           []string{"<em>"},
		PostTags:          []string{"</em>"},
	}
	for _, opt := range options {
		opt.apply(cfg)
	}

	fieldMap := make(map[string]any)
	for _, field := range fields {
		fieldMap[field] = Map{}
	}
	return BuildMap("fields", fieldMap,
		"fragment_size", cfg.fragmentSize,
		"number_of_fragments", cfg.numberOfFragments,
		"pre_tags", cfg.PreTags,
		"post_tags", cfg.PostTags,
	)
}

// BuildMap 用于构造map[string]any的辅助方法
// kvs 为连续的 key-value 对，key 必须是 string 类型，如 ["key", "value", "key2", "value2", ...]
func BuildMap(kvs ...any) Map {
	m := make(map[string]any)
	for i := 0; i < len(kvs); i += 2 {
		if i+1 >= len(kvs) {
			// 如果没有成对，跳过
			break
		}
		key, ok := kvs[i].(string)
		if !ok {
			// 如果 key 不是 string 类型，则跳过这一对
			continue
		}
		// 直接把合法的 key 和 value 加入 map
		m[key] = kvs[i+1]
	}
	return m
}
