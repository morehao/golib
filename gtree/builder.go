package gtree

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
type Comparator[T TreeNode[K], K comparable] interface {
	Compare(a, b T) int // 返回 -1: a<b, 0: a==b, 1: a>b
}

// OrphanStrategy 孤儿节点处理策略
type OrphanStrategy int

const (
	// IgnoreOrphans 忽略孤儿节点（默认）
	IgnoreOrphans OrphanStrategy = iota
	// CollectOrphans 将孤儿节点收集为根节点
	CollectOrphans
	// ErrorOnOrphans 遇到孤儿节点时触发错误处理
	ErrorOnOrphans
)

// TreeBuilder 通用树构建器
type TreeBuilder[K comparable, N TreeNode[K]] struct {
	ctx context.Context
	// comparator 节点比较器，可选
	comparator Comparator[N, K]
	// errorHandler 错误处理函数
	errorHandler func(ctx context.Context, nodeKey, parentKey K, err error)
	// orphanStrategy 孤儿节点处理策略
	orphanStrategy OrphanStrategy
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
func WithComparator[K comparable, N TreeNode[K]](comp Comparator[N, K]) Option[K, N] {
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

// WithOrphanStrategy 设置孤儿节点处理策略
func WithOrphanStrategy[K comparable, N TreeNode[K]](strategy OrphanStrategy) Option[K, N] {
	return func(b *TreeBuilder[K, N]) {
		b.orphanStrategy = strategy
	}
}

// NewTreeBuilder 创建新的树构建器
func NewTreeBuilder[K comparable, N TreeNode[K]](opts ...Option[K, N]) *TreeBuilder[K, N] {
	builder := &TreeBuilder[K, N]{
		ctx:            context.Background(),
		orphanStrategy: IgnoreOrphans,
		errorHandler: func(ctx context.Context, nodeKey, parentKey K, err error) {
			fmt.Printf("[WARN] Orphan node detected: node %v references missing parent %v: %v", nodeKey, parentKey, err)
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
		nodeMap[node.GetKey()] = node
	}

	// 构建树结构
	var roots []N
	for i := range nodes {
		node := nodes[i]
		if node.IsRoot() {
			// 初始化根节点的子节点切片
			node.SetChildren([]TreeNode[K]{})
			roots = append(roots, node)
		} else {
			parentKey := node.GetParentKey()
			if parent, exists := nodeMap[parentKey]; exists {
				// 初始化当前节点的子节点切片
				node.SetChildren([]TreeNode[K]{})

				// 将当前节点添加到父节点的子节点列表
				children := parent.GetChildren()
				parent.SetChildren(append(children, node))
			} else {
				// 处理孤儿节点
				b.handleOrphanNode(node, parentKey, &roots)
			}
		}
	}

	// 排序（如果设置了比较器）
	if b.comparator != nil {
		b.sortTreeRecursive(roots)
	}

	return roots
}

// handleOrphanNode 处理孤儿节点
func (b *TreeBuilder[K, N]) handleOrphanNode(node N, parentKey K, roots *[]N) {
	switch b.orphanStrategy {
	case CollectOrphans:
		// 将孤儿节点作为根节点收集
		node.SetChildren([]TreeNode[K]{})
		*roots = append(*roots, node)
	case ErrorOnOrphans:
		// 触发错误处理
		b.errorHandler(b.ctx, node.GetKey(), parentKey, fmt.Errorf("parent not found"))
	case IgnoreOrphans:
		// 默认：触发错误处理但不收集
		b.errorHandler(b.ctx, node.GetKey(), parentKey, fmt.Errorf("parent not found"))
	}
}

// sortTreeRecursive 递归排序树节点
func (b *TreeBuilder[K, N]) sortTreeRecursive(nodes []N) {
	if b.comparator == nil || len(nodes) == 0 {
		return
	}

	// 排序当前层级
	sort.Slice(nodes, func(i, j int) bool {
		return b.comparator.Compare(nodes[i], nodes[j]) < 0
	})

	// 递归排序子节点
	for _, node := range nodes {
		children := node.GetChildren()
		if len(children) == 0 {
			continue
		}

		// 直接在接口切片上排序
		sort.Slice(children, func(i, j int) bool {
			return b.comparator.Compare(children[i].(N), children[j].(N)) < 0
		})

		// 转换为具体类型进行递归
		typedChildren := make([]N, len(children))
		for i, child := range children {
			typedChildren[i] = child.(N)
		}
		b.sortTreeRecursive(typedChildren)
	}
}

// GetNodesByLevel 按层级返回节点，返回 map[层级][]节点
// level 0 为根节点层
func (b *TreeBuilder[K, N]) GetNodesByLevel(roots []N) map[int][]N {
	result := make(map[int][]N)
	if len(roots) == 0 {
		return result
	}

	b.traverseByLevel(roots, 0, result)
	return result
}

// traverseByLevel 递归遍历并按层级收集节点
func (b *TreeBuilder[K, N]) traverseByLevel(nodes []N, level int, result map[int][]N) {
	if len(nodes) == 0 {
		return
	}

	// 收集当前层级的节点
	result[level] = append(result[level], nodes...)

	// 收集所有子节点
	var nextLevel []N
	for _, node := range nodes {
		children := node.GetChildren()
		for _, child := range children {
			nextLevel = append(nextLevel, child.(N))
		}
	}

	// 递归处理下一层
	if len(nextLevel) > 0 {
		b.traverseByLevel(nextLevel, level+1, result)
	}
}

// GetMaxLevel 获取树的最大层级（根节点为 0）
func (b *TreeBuilder[K, N]) GetMaxLevel(roots []N) int {
	if len(roots) == 0 {
		return -1
	}
	return b.getMaxLevelRecursive(roots, 0)
}

// getMaxLevelRecursive 递归获取最大层级
func (b *TreeBuilder[K, N]) getMaxLevelRecursive(nodes []N, currentLevel int) int {
	maxLevel := currentLevel

	for _, node := range nodes {
		children := node.GetChildren()
		if len(children) > 0 {
			typedChildren := make([]N, len(children))
			for i, child := range children {
				typedChildren[i] = child.(N)
			}
			childMaxLevel := b.getMaxLevelRecursive(typedChildren, currentLevel+1)
			if childMaxLevel > maxLevel {
				maxLevel = childMaxLevel
			}
		}
	}

	return maxLevel
}

// BuildWithMap 构建树结构并返回节点映射表
func (b *TreeBuilder[K, N]) BuildWithMap(nodes []N) ([]N, map[K]N) {
	if len(nodes) == 0 {
		return []N{}, make(map[K]N)
	}

	// 创建节点映射
	nodeMap := make(map[K]N, len(nodes))
	for i := range nodes {
		node := nodes[i]
		nodeMap[node.GetKey()] = node
	}

	roots := b.Build(nodes)
	return roots, nodeMap
}

// ============ 常用比较器实现 ============

// IDComparator 基于 ID 字段的比较器（需要节点实现 GetID 方法）
type IDComparator[T interface {
	TreeNode[K]
	GetID() uint
}, K comparable] struct{}

func (c IDComparator[T, K]) Compare(a, b T) int {
	aID, bID := a.GetID(), b.GetID()
	if aID < bID {
		return -1
	} else if aID > bID {
		return 1
	}
	return 0
}

// NameComparator 基于 Name 字段的比较器
type NameComparator[T interface {
	TreeNode[K]
	GetName() string
}, K comparable] struct{}

func (c NameComparator[T, K]) Compare(a, b T) int {
	aName, bName := a.GetName(), b.GetName()
	if aName < bName {
		return -1
	} else if aName > bName {
		return 1
	}
	return 0
}

// CompositeComparator 组合比较器，支持多级排序
type CompositeComparator[T TreeNode[K], K comparable] struct {
	comparators []Comparator[T, K]
}

func NewCompositeComparator[T TreeNode[K], K comparable](comparators ...Comparator[T, K]) *CompositeComparator[T, K] {
	return &CompositeComparator[T, K]{comparators: comparators}
}

func (c *CompositeComparator[T, K]) Compare(a, b T) int {
	for _, comp := range c.comparators {
		if result := comp.Compare(a, b); result != 0 {
			return result
		}
	}
	return 0
}

// OrderComparator 基于 Order 字段的比较器（升序）
type OrderComparator[T interface {
	TreeNode[K]
	GetOrder() int
}, K comparable] struct{}

func (c OrderComparator[T, K]) Compare(a, b T) int {
	aOrder, bOrder := a.GetOrder(), b.GetOrder()
	if aOrder < bOrder {
		return -1
	} else if aOrder > bOrder {
		return 1
	}
	return 0
}
