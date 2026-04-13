package gtree

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"sort"
)

// =============================================================================
// 核心接口
// =============================================================================

// TreeNode 节点只需提供键和关系查询，不再持有子节点列表。
// 约束：N 必须为指针类型，否则 Build 时对节点的修改不会反映到原始数据。
type TreeNode[K comparable] interface {
	GetKey() K
	GetParentKey() K
	IsRoot() bool
}

// Comparator 节点排序比较器
type Comparator[N any] interface {
	Compare(a, b N) int // -1: a<b, 0: a==b, 1: a>b
}

// ComparatorFunc 函数式比较器，方便内联使用
type ComparatorFunc[N any] func(a, b N) int

func (f ComparatorFunc[N]) Compare(a, b N) int { return f(a, b) }

// =============================================================================
// 孤儿策略
// =============================================================================

// OrphanStrategy 孤儿节点（父节点不存在）处理策略
type OrphanStrategy int

const (
	IgnoreOrphans  OrphanStrategy = iota // 静默丢弃
	CollectOrphans                       // 提升为根节点
	ErrorOnOrphans                       // 调用 errorHandler，然后丢弃
)

// =============================================================================
// BuildError：构建过程中的错误信息
// =============================================================================

// ErrorKind 错误类型
type ErrorKind int

const (
	ErrDuplicateKey ErrorKind = iota // 重复的 key
	ErrOrphanNode                    // 孤儿节点
	ErrCyclicGraph                   // 存在循环引用
	ErrContextDone                   // context 已取消
)

func (e ErrorKind) String() string {
	switch e {
	case ErrDuplicateKey:
		return "duplicate key"
	case ErrOrphanNode:
		return "orphan node"
	case ErrCyclicGraph:
		return "cyclic graph"
	case ErrContextDone:
		return "context done"
	default:
		return "unknown"
	}
}

// 哨兵错误，支持 errors.Is / errors.As 判断
var (
	ErrKindDuplicateKey = errors.New("duplicate key")
	ErrKindOrphanNode   = errors.New("orphan node")
	ErrKindCyclicGraph  = errors.New("cyclic graph")
	ErrKindContextDone  = errors.New("context done")
)

func sentinelFor(k ErrorKind) error {
	switch k {
	case ErrDuplicateKey:
		return ErrKindDuplicateKey
	case ErrOrphanNode:
		return ErrKindOrphanNode
	case ErrCyclicGraph:
		return ErrKindCyclicGraph
	case ErrContextDone:
		return ErrKindContextDone
	default:
		return fmt.Errorf("unknown error kind %d", k)
	}
}

// BuildError 记录一次构建错误的上下文
type BuildError[K comparable] struct {
	Kind      ErrorKind
	NodeKey   K
	ParentKey K
	Err       error // 始终为对应的哨兵错误，支持 errors.Is
}

func (e *BuildError[K]) Error() string {
	return fmt.Sprintf("[%s] node=%v parent=%v: %v", e.Kind, e.NodeKey, e.ParentKey, e.Err)
}

func (e *BuildError[K]) Unwrap() error { return e.Err }

func newBuildError[K comparable](kind ErrorKind, nodeKey, parentKey K) *BuildError[K] {
	return &BuildError[K]{
		Kind:      kind,
		NodeKey:   nodeKey,
		ParentKey: parentKey,
		Err:       sentinelFor(kind),
	}
}

// =============================================================================
// Tree：构建结果，持有树结构和所有查询方法
// =============================================================================

// Tree 持有构建完成的树：根节点列表 + 父子关系表
type Tree[K comparable, N TreeNode[K]] struct {
	// Roots 根节点（含被提升的孤儿节点，取决于策略）
	Roots []N
	// NodeMap 全量节点索引，key → node
	NodeMap map[K]N
	// childrenMap 父子关系：parentKey → []childNode（内部持有，通过方法访问）
	childrenMap map[K][]N
	// BuildErrors 构建过程中收集到的所有错误
	BuildErrors []*BuildError[K]
}

// Children 返回某节点的直接子节点副本，防止外部修改破坏内部结构。
// 第二个返回值表示该 key 是否存在于树中，可区分"key 不存在"与"叶子节点"两种情况。
func (t *Tree[K, N]) Children(key K) ([]N, bool) {
	if _, ok := t.NodeMap[key]; !ok {
		return nil, false
	}
	children := t.childrenMap[key]
	if len(children) == 0 {
		return nil, true
	}
	result := make([]N, len(children))
	copy(result, children)
	return result, true
}

// bfsByLevel 内部通用 BFS，按层序对每层节点执行 fn(level, nodes)。
// fn 返回 false 时提前终止。
// 入队时即标记 visited，防止同层重复节点被重复计入。
func (t *Tree[K, N]) bfsByLevel(fn func(level int, nodes []N) bool) {
	if len(t.Roots) == 0 {
		return
	}
	visited := make(map[K]bool, len(t.NodeMap))
	queue := make([]N, 0, len(t.Roots))
	for _, root := range t.Roots {
		key := root.GetKey()
		if !visited[key] {
			visited[key] = true
			queue = append(queue, root)
		}
	}
	for level := 0; len(queue) > 0; level++ {
		if !fn(level, queue) {
			return
		}
		var next []N
		for _, node := range queue {
			for _, child := range t.childrenMap[node.GetKey()] {
				ck := child.GetKey()
				if !visited[ck] {
					visited[ck] = true
					next = append(next, child)
				}
			}
		}
		queue = next
	}
}

// GetNodesByLevel 按层级返回节点，level 0 为根节点层。
func (t *Tree[K, N]) GetNodesByLevel() map[int][]N {
	result := make(map[int][]N)
	t.bfsByLevel(func(level int, nodes []N) bool {
		result[level] = append(result[level], nodes...)
		return true
	})
	return result
}

// MaxLevel 返回树的最大深度（根节点为 0；空树返回 -1）。
func (t *Tree[K, N]) MaxLevel() int {
	maxLevel := -1
	t.bfsByLevel(func(level int, _ []N) bool {
		maxLevel = level
		return true
	})
	return maxLevel
}

// Walk 前序遍历整棵树，fn 返回 false 时停止遍历。
//
// visited 检查为防御性保护：正常构建流程不会产生共享子节点，
// 但当外部代码直接操作 childrenMap（或未来内部逻辑变更）时，
// 可防止节点被重复遍历。
func (t *Tree[K, N]) Walk(fn func(node N, level int) bool) {
	type frame struct {
		node  N
		level int
	}
	visited := make(map[K]bool, len(t.NodeMap))
	stack := make([]frame, 0, len(t.Roots))
	for i := len(t.Roots) - 1; i >= 0; i-- {
		stack = append(stack, frame{t.Roots[i], 0})
	}
	for len(stack) > 0 {
		top := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		key := top.node.GetKey()
		if visited[key] {
			continue
		}
		visited[key] = true

		if !fn(top.node, top.level) {
			return
		}
		children := t.childrenMap[key]
		for i := len(children) - 1; i >= 0; i-- {
			if !visited[children[i].GetKey()] {
				stack = append(stack, frame{children[i], top.level + 1})
			}
		}
	}
}

// Filter 返回满足条件的所有节点（前序遍历）。
func (t *Tree[K, N]) Filter(predicate func(N) bool) []N {
	var result []N
	t.Walk(func(node N, _ int) bool {
		if predicate(node) {
			result = append(result, node)
		}
		return true
	})
	return result
}

// =============================================================================
// TreeBuilder：配置 + 构建
// =============================================================================

// TreeBuilder 树构建器，通过 Option 配置行为
type TreeBuilder[K comparable, N TreeNode[K]] struct {
	ctx            context.Context
	comparator     Comparator[N]
	errorHandler   func(ctx context.Context, err *BuildError[K])
	orphanStrategy OrphanStrategy
}

// Option 构建器选项
type Option[K comparable, N TreeNode[K]] func(*TreeBuilder[K, N])

func WithContext[K comparable, N TreeNode[K]](ctx context.Context) Option[K, N] {
	return func(b *TreeBuilder[K, N]) { b.ctx = ctx }
}

func WithComparator[K comparable, N TreeNode[K]](comp Comparator[N]) Option[K, N] {
	return func(b *TreeBuilder[K, N]) { b.comparator = comp }
}

func WithErrorHandler[K comparable, N TreeNode[K]](h func(context.Context, *BuildError[K])) Option[K, N] {
	return func(b *TreeBuilder[K, N]) { b.errorHandler = h }
}

func WithOrphanStrategy[K comparable, N TreeNode[K]](s OrphanStrategy) Option[K, N] {
	return func(b *TreeBuilder[K, N]) { b.orphanStrategy = s }
}

// NewTreeBuilder 创建树构建器
func NewTreeBuilder[K comparable, N TreeNode[K]](opts ...Option[K, N]) *TreeBuilder[K, N] {
	b := &TreeBuilder[K, N]{
		ctx:            context.Background(),
		orphanStrategy: IgnoreOrphans,
		errorHandler:   func(_ context.Context, _ *BuildError[K]) {},
	}
	for _, opt := range opts {
		opt(b)
	}
	return b
}

// Build 构建树，返回 *Tree 持有全部结果。
//
// 处理顺序：
//  1. 建立节点索引（检测重复 key），同时记录有序 key 列表
//  2. 按原始切片顺序建立父子关系 & 收集根节点（处理孤儿策略）
//  3. 检测循环引用（有环时报告错误并移除对应边）
//  4. 按 comparator 排序
//
// 每个阶段开始前检查 context 是否已取消，取消时附带错误提前返回。
func (b *TreeBuilder[K, N]) Build(nodes []N) *Tree[K, N] {
	tree := &Tree[K, N]{
		NodeMap:     make(map[K]N, len(nodes)),
		childrenMap: make(map[K][]N),
	}
	if len(nodes) == 0 {
		return tree
	}

	// 1. 建立节点索引，检测重复 key。
	// 重复的 key 仅记录错误，保留先出现的节点，后续步骤跳过重复节点。
	// orderedKeys 记录去重后的输入顺序，传递给 removeCycles 使用，
	// 避免将其挂在 Tree 结构体上造成语义混乱。
	duplicates := make(map[K]bool)
	orderedKeys := make([]K, 0, len(nodes))

	for _, node := range nodes {
		// 阶段开头检查 context
		if err := b.ctx.Err(); err != nil {
			b.appendContextError(tree, err)
			return tree
		}

		key := node.GetKey()
		if _, exists := tree.NodeMap[key]; exists {
			duplicates[key] = true
			// 【修复2】重复 key 时 parentKey 未知，传零值避免误导
			var zeroK K
			e := newBuildError[K](ErrDuplicateKey, key, zeroK)
			tree.BuildErrors = append(tree.BuildErrors, e)
			b.errorHandler(b.ctx, e)
			continue
		}
		tree.NodeMap[key] = node
		orderedKeys = append(orderedKeys, key)
	}

	// 2. 按原始切片顺序遍历，建立父子关系 & 收集根节点。
	for _, node := range nodes {
		if err := b.ctx.Err(); err != nil {
			b.appendContextError(tree, err)
			return tree
		}

		key := node.GetKey()
		if duplicates[key] {
			continue
		}
		if node.IsRoot() {
			tree.Roots = append(tree.Roots, node)
			continue
		}
		parentKey := node.GetParentKey()
		if _, exists := tree.NodeMap[parentKey]; exists {
			tree.childrenMap[parentKey] = append(tree.childrenMap[parentKey], node)
		} else {
			b.handleOrphan(tree, node, parentKey)
		}
	}

	// 3. 检测循环引用，移除有环的边并报告错误
	if err := b.ctx.Err(); err != nil {
		b.appendContextError(tree, err)
		return tree
	}
	b.removeCycles(tree, orderedKeys)

	// 4. 排序
	if b.comparator != nil {
		if err := b.ctx.Err(); err != nil {
			b.appendContextError(tree, err)
			return tree
		}
		b.sortByLevel(tree)
	}

	return tree
}

// appendContextError 将 context 取消错误追加到 BuildErrors 并调用 errorHandler。
func (b *TreeBuilder[K, N]) appendContextError(tree *Tree[K, N], _ error) {
	var zeroK K
	e := newBuildError[K](ErrContextDone, zeroK, zeroK)
	tree.BuildErrors = append(tree.BuildErrors, e)
	b.errorHandler(b.ctx, e)
}

func (b *TreeBuilder[K, N]) handleOrphan(tree *Tree[K, N], node N, parentKey K) {
	switch b.orphanStrategy {
	case CollectOrphans:
		tree.Roots = append(tree.Roots, node)
	case ErrorOnOrphans:
		e := newBuildError(ErrOrphanNode, node.GetKey(), parentKey)
		tree.BuildErrors = append(tree.BuildErrors, e)
		b.errorHandler(b.ctx, e)
		// 丢弃：不加入 Roots，也不加入 childrenMap
	case IgnoreOrphans:
		// 静默丢弃
	}
}

// removeCycles 使用迭代 DFS 检测有向环，发现环时移除形成环的那条边（后向边）并报告错误。
//
// 【修复1】orderedKeys 由 Build 传入（输入顺序去重列表），不再挂在 Tree 上。
// 补充遍历孤立闭合环时按此顺序迭代，保证对同一输入结果完全确定。
//
// 【修复：栈顶访问】所有对栈顶的读写均通过下标 stack[topIdx] 完成，
// 不持有跨 append 的指针，彻底消除悬挂指针风险。
func (b *TreeBuilder[K, N]) removeCycles(tree *Tree[K, N], orderedKeys []K) {
	const (
		stateUnvisited uint8 = 0
		stateInStack   uint8 = 1
		stateDone      uint8 = 2
	)

	visited := make(map[K]uint8, len(tree.NodeMap))

	type frame struct {
		key      K
		childIdx int
	}

	var dfs func(startKey K)
	dfs = func(startKey K) {
		if visited[startKey] != stateUnvisited {
			return
		}

		stack := []frame{{key: startKey, childIdx: 0}}
		visited[startKey] = stateInStack

		for len(stack) > 0 {
			// 【修复：栈顶访问】始终通过下标访问栈顶，不持有跨迭代的指针。
			topIdx := len(stack) - 1
			// 每次从 map 取最新 slice，避免同路径上多次删边后与缓存不一致。
			children := tree.childrenMap[stack[topIdx].key]

			if stack[topIdx].childIdx >= len(children) {
				visited[stack[topIdx].key] = stateDone
				stack = stack[:topIdx]
				continue
			}

			child := children[stack[topIdx].childIdx]
			ck := child.GetKey()
			stack[topIdx].childIdx++

			switch visited[ck] {
			case stateUnvisited:
				visited[ck] = stateInStack
				// append 后 topIdx 仍有效（指向 append 前的最后一个元素），
				// 但下一轮循环会重新计算 topIdx，无需担心。
				stack = append(stack, frame{key: ck, childIdx: 0})

			case stateInStack:
				// 后向边：形成环，移除该边并报告错误。
				e := newBuildError(ErrCyclicGraph, ck, stack[topIdx].key)
				tree.BuildErrors = append(tree.BuildErrors, e)
				b.errorHandler(b.ctx, e)

				// edgeIdx 是刚才处理的子节点在当前 slice 中的下标（childIdx 已自增）。
				edgeIdx := stack[topIdx].childIdx - 1

				// 重新从 map 取最新 slice，构造移除后的新 slice。
				cur := tree.childrenMap[stack[topIdx].key]
				newChildren := make([]N, 0, len(cur)-1)
				newChildren = append(newChildren, cur[:edgeIdx]...)
				newChildren = append(newChildren, cur[edgeIdx+1:]...)
				tree.childrenMap[stack[topIdx].key] = newChildren

				// 移除后原 edgeIdx+1 的元素现在位于 edgeIdx，
				// 而 childIdx 已指向 edgeIdx+1，回退一位使其重新指向 edgeIdx。
				stack[topIdx].childIdx--

			case stateDone:
				// cross/forward edge，正常保留
			}
		}
	}

	// 先从所有显式根节点出发
	for _, root := range tree.Roots {
		dfs(root.GetKey())
	}

	// 【修复1】按输入顺序补充遍历，处理未被根可达的孤立闭合环。
	// 使用 orderedKeys（Build 传入）而非 NodeMap 迭代，保证确定性。
	for _, key := range orderedKeys {
		dfs(key)
	}
}

// sortByLevel 使用 BFS 层序遍历对每层子节点排序，避免递归导致的栈溢出。
func (b *TreeBuilder[K, N]) sortByLevel(tree *Tree[K, N]) {
	sort.Slice(tree.Roots, func(i, j int) bool {
		return b.comparator.Compare(tree.Roots[i], tree.Roots[j]) < 0
	})

	visited := make(map[K]bool, len(tree.NodeMap))
	queue := make([]K, 0, len(tree.Roots))
	for _, root := range tree.Roots {
		key := root.GetKey()
		if !visited[key] {
			visited[key] = true
			queue = append(queue, key)
		}
	}

	for len(queue) > 0 {
		var next []K
		for _, key := range queue {
			children := tree.childrenMap[key]
			if len(children) == 0 {
				continue
			}
			sort.Slice(children, func(i, j int) bool {
				return b.comparator.Compare(children[i], children[j]) < 0
			})
			for _, child := range children {
				ck := child.GetKey()
				if !visited[ck] {
					visited[ck] = true
					next = append(next, ck)
				}
			}
		}
		queue = next
	}
}

// =============================================================================
// 内置比较器
// =============================================================================

// IDComparator 按 uint ID 升序
type IDComparator[N interface {
	TreeNode[K]
	GetID() uint
}, K comparable] struct{}

func (c IDComparator[N, K]) Compare(a, b N) int {
	return cmp.Compare(a.GetID(), b.GetID())
}

// NameComparator 按字符串 Name 升序
type NameComparator[N interface {
	TreeNode[K]
	GetName() string
}, K comparable] struct{}

func (c NameComparator[N, K]) Compare(a, b N) int {
	return cmp.Compare(a.GetName(), b.GetName())
}

// OrderComparator 按 int Order 升序
type OrderComparator[N interface {
	TreeNode[K]
	GetOrder() int
}, K comparable] struct{}

func (c OrderComparator[N, K]) Compare(a, b N) int {
	return cmp.Compare(a.GetOrder(), b.GetOrder())
}

// CompositeComparator 多级排序，按顺序依次比较直到分出大小
type CompositeComparator[N any] struct {
	comparators []Comparator[N]
}

func NewCompositeComparator[N any](comparators ...Comparator[N]) *CompositeComparator[N] {
	return &CompositeComparator[N]{comparators: comparators}
}

func (c *CompositeComparator[N]) Compare(a, b N) int {
	for _, comp := range c.comparators {
		if r := comp.Compare(a, b); r != 0 {
			return r
		}
	}
	return 0
}
