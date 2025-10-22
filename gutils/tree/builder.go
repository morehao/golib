package tree

import (
	"context"
	"fmt"
	"sort"
)

// TreeNode 树节点接口，任何需要构建树的结构都需要实现此接口
type TreeNode[K comparable] interface {
	// GetKey 获取节点唯一标识
	GetKey() K
	// GetParentKey 获取父节点标识
	GetParentKey() K
	// SetChildren 设置子节点
	SetChildren(children []TreeNode[K])
	// GetChildren 获取子节点
	GetChildren() []TreeNode[K]
	// IsRoot 判断是否为根节点
	IsRoot() bool
}

// Comparator 比较器接口，用于自定义排序逻辑
type Comparator[T any] interface {
	Compare(a, b T) int // 返回 -1: a<b, 0: a==b, 1: a>b
}

// TreeBuilder 通用树构建器
type TreeBuilder[K comparable, N TreeNode[K]] struct {
	ctx context.Context
	// comparator 节点比较器，可选
	comparator Comparator[N]
	// errorHandler 错误处理函数
	errorHandler func(ctx context.Context, nodeKey, parentKey K, err error)
}

// Option 构建器选项
type Option[K comparable, N TreeNode[K]] func(*TreeBuilder[K, N])

// WithContext 设置上下文
func WithContext[K comparable, N TreeNode[K]](ctx context.Context) Option[K, N] {
	return func(b *TreeBuilder[K, N]) {
		b.ctx = ctx
	}
}

// WithComparator 设置比较器
func WithComparator[K comparable, N TreeNode[K]](comp Comparator[N]) Option[K, N] {
	return func(b *TreeBuilder[K, N]) {
		b.comparator = comp
	}
}

// WithErrorHandler 设置错误处理函数
func WithErrorHandler[K comparable, N TreeNode[K]](handler func(ctx context.Context, nodeKey, parentKey K, err error)) Option[K, N] {
	return func(b *TreeBuilder[K, N]) {
		b.errorHandler = handler
	}
}

// NewTreeBuilder 创建新的树构建器
func NewTreeBuilder[K comparable, N TreeNode[K]](opts ...Option[K, N]) *TreeBuilder[K, N] {
	builder := &TreeBuilder[K, N]{
		ctx: context.Background(),
		errorHandler: func(ctx context.Context, nodeKey, parentKey K, err error) {
			fmt.Printf("Warning: parent node %v of node %v not found: %v\n", nodeKey, parentKey, err)
		},
	}

	for _, opt := range opts {
		opt(builder)
	}

	return builder
}

// Build 构建树结构
func (b *TreeBuilder[K, N]) Build(nodes []N) []N {
	if len(nodes) == 0 {
		return []N{}
	}

	// 创建节点映射
	nodeMap := make(map[K]N, len(nodes))
	for i := range nodes {
		node := nodes[i]
		node.SetChildren([]TreeNode[K]{}) // 初始化子节点
		nodeMap[node.GetKey()] = node
	}

	// 构建树结构
	var roots []N
	for i := range nodes {
		node := nodes[i]
		if node.IsRoot() {
			roots = append(roots, node)
		} else {
			parentKey := node.GetParentKey()
			if parent, exists := nodeMap[parentKey]; exists {
				children := parent.GetChildren()
				// 类型转换
				parent.SetChildren(append(children, node))
			} else {
				b.errorHandler(b.ctx, node.GetKey(), parentKey, fmt.Errorf("parent not found"))
			}
		}
	}

	// 排序
	if b.comparator != nil {
		b.sortNodes(roots)
		b.sortChildrenRecursive(roots)
	}

	return roots
}

// sortNodes 对节点切片排序
func (b *TreeBuilder[K, N]) sortNodes(nodes []N) {
	if b.comparator == nil {
		return
	}
	sort.Slice(nodes, func(i, j int) bool {
		return b.comparator.Compare(nodes[i], nodes[j]) < 0
	})
}

// sortChildrenRecursive 递归排序子节点
func (b *TreeBuilder[K, N]) sortChildrenRecursive(nodes []N) {
	for _, node := range nodes {
		children := node.GetChildren()
		if len(children) == 0 {
			continue
		}

		// 转换为具体类型进行排序
		typedChildren := make([]N, len(children))
		for i, child := range children {
			typedChildren[i] = child.(N)
		}

		b.sortNodes(typedChildren)

		// 转换回接口类型
		interfaceChildren := make([]TreeNode[K], len(typedChildren))
		for i, child := range typedChildren {
			interfaceChildren[i] = child
		}
		node.SetChildren(interfaceChildren)

		// 递归排序
		b.sortChildrenRecursive(typedChildren)
	}
}

// ============ 常用比较器实现 ============

// IDComparator 基于 ID 字段的比较器（需要节点实现 GetID 方法）
type IDComparator[T interface{ GetID() uint }] struct{}

func (c IDComparator[T]) Compare(a, b T) int {
	aID, bID := a.GetID(), b.GetID()
	if aID < bID {
		return -1
	} else if aID > bID {
		return 1
	}
	return 0
}

// NameComparator 基于 Name 字段的比较器
type NameComparator[T interface{ GetName() string }] struct{}

func (c NameComparator[T]) Compare(a, b T) int {
	aName, bName := a.GetName(), b.GetName()
	if aName < bName {
		return -1
	} else if aName > bName {
		return 1
	}
	return 0
}

// CompositeComparator 组合比较器，支持多级排序
type CompositeComparator[T any] struct {
	comparators []Comparator[T]
}

func NewCompositeComparator[T any](comparators ...Comparator[T]) *CompositeComparator[T] {
	return &CompositeComparator[T]{comparators: comparators}
}

func (c *CompositeComparator[T]) Compare(a, b T) int {
	for _, comp := range c.comparators {
		if result := comp.Compare(a, b); result != 0 {
			return result
		}
	}
	return 0
}
