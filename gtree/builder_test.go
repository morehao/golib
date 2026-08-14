package gtree

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

type testNode struct {
	nodeKey   int
	parentKey int
	isRoot    bool

	sortID    uint
	sortName  string
	sortOrder int
}

func (n *testNode) GetKey() int       { return n.nodeKey }
func (n *testNode) GetParentKey() int { return n.parentKey }
func (n *testNode) IsRoot() bool      { return n.isRoot }
func (n *testNode) GetID() uint       { return n.sortID }
func (n *testNode) GetName() string   { return n.sortName }
func (n *testNode) GetOrder() int     { return n.sortOrder }

func node(key, parent int, root bool) *testNode {
	return &testNode{
		nodeKey:   key,
		parentKey: parent,
		isRoot:    root,
		sortID:    uint(key),
		sortName:  string(rune('a' + key)),
		sortOrder: key,
	}
}

func keysOf(nodes []*testNode) []int {
	out := make([]int, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, n.GetKey())
	}
	return out
}

func printTestTree(tree *Tree[int, *testNode]) string {
	var sb strings.Builder
	sb.WriteString("Tree Structure:\n")

	visited := make(map[int]bool)

	var printNode func(node *testNode, prefix string, isLast bool)
	printNode = func(node *testNode, prefix string, isLast bool) {
		if visited[node.nodeKey] {
			return
		}
		visited[node.nodeKey] = true

		connector := "└─ "
		if !isLast {
			connector = "├─ "
		}

		sb.WriteString(fmt.Sprintf("%s%s[%d] %q\n", prefix, connector, node.nodeKey, node.sortName))

		children, _ := tree.Children(node.nodeKey)
		if children == nil {
			return
		}

		newPrefix := prefix
		if isLast {
			newPrefix += "   "
		} else {
			newPrefix += "│  "
		}

		for i, child := range children {
			printNode(child, newPrefix, i == len(children)-1)
		}
	}

	for i, root := range tree.Roots {
		printNode(root, "", i == len(tree.Roots)-1)
	}

	return sb.String()
}

func TestBuildBasicTreeAndQueries_Roots(t *testing.T) {
	nodes := []*testNode{
		node(1, 0, true),
		node(2, 1, false),
		node(3, 1, false),
		node(4, 2, false),
	}

	tree := NewTreeBuilder[int, *testNode]().Build(nodes)
	t.Log(printTestTree(tree))

	assert.Equal(t, []int{1}, keysOf(tree.Roots))
}

func TestBuildBasicTreeAndQueries_Children(t *testing.T) {
	nodes := []*testNode{
		node(1, 0, true),
		node(2, 1, false),
		node(3, 1, false),
		node(4, 2, false),
	}

	tree := NewTreeBuilder[int, *testNode]().Build(nodes)
	t.Log(printTestTree(tree))

	children1, ok := tree.Children(1)
	assert.True(t, ok)
	assert.Equal(t, []int{2, 3}, keysOf(children1))

	children3, ok := tree.Children(3)
	assert.True(t, ok)
	assert.Nil(t, children3)

	childrenX, ok := tree.Children(999)
	assert.False(t, ok)
	assert.Nil(t, childrenX)
}

func TestBuildBasicTreeAndQueries_GetNodesByLevel(t *testing.T) {
	nodes := []*testNode{
		node(1, 0, true),
		node(2, 1, false),
		node(3, 1, false),
		node(4, 2, false),
	}

	tree := NewTreeBuilder[int, *testNode]().Build(nodes)
	t.Log(printTestTree(tree))

	levels := tree.GetNodesByLevel()
	assert.Equal(t, []int{1}, keysOf(levels[0]))
	assert.Equal(t, []int{2, 3}, keysOf(levels[1]))
	assert.Equal(t, []int{4}, keysOf(levels[2]))
}

func TestBuildBasicTreeAndQueries_MaxLevel(t *testing.T) {
	nodes := []*testNode{
		node(1, 0, true),
		node(2, 1, false),
		node(3, 1, false),
		node(4, 2, false),
	}

	tree := NewTreeBuilder[int, *testNode]().Build(nodes)
	t.Log(printTestTree(tree))

	assert.Equal(t, 2, tree.MaxLevel())
}

func TestBuildBasicTreeAndQueries_Walk(t *testing.T) {
	nodes := []*testNode{
		node(1, 0, true),
		node(2, 1, false),
		node(3, 1, false),
		node(4, 2, false),
	}

	tree := NewTreeBuilder[int, *testNode]().Build(nodes)
	t.Log(printTestTree(tree))

	var walked []int
	tree.Walk(func(n *testNode, level int) bool {
		_ = level
		walked = append(walked, n.GetKey())
		return true
	})
	assert.Equal(t, []int{1, 2, 4, 3}, walked)

	var early []int
	tree.Walk(func(n *testNode, level int) bool {
		_ = level
		early = append(early, n.GetKey())
		return len(early) < 2
	})
	assert.Equal(t, []int{1, 2}, early)
}

func TestBuildBasicTreeAndQueries_Filter(t *testing.T) {
	nodes := []*testNode{
		node(1, 0, true),
		node(2, 1, false),
		node(3, 1, false),
		node(4, 2, false),
	}

	tree := NewTreeBuilder[int, *testNode]().Build(nodes)
	t.Log(printTestTree(tree))

	filtered := tree.Filter(func(n *testNode) bool { return n.GetKey()%2 == 0 })
	assert.Equal(t, []int{2, 4}, keysOf(filtered))
}

func TestBuildDuplicateKeyReportsErrorAndKeepsFirstNode(t *testing.T) {
	first := node(2, 1, false)
	second := node(2, 1, false)

	var handled []*BuildError[int]
	b := NewTreeBuilder[int, *testNode](WithErrorHandler[int, *testNode](func(_ context.Context, err *BuildError[int]) {
		handled = append(handled, err)
	}))

	tree := b.Build([]*testNode{
		node(1, 0, true),
		first,
		node(3, 1, false),
		second,
	})

	assert.Equal(t, first, tree.NodeMap[2], "NodeMap[2] should keep first node")
	assert.Len(t, tree.BuildErrors, 1)

	err := tree.BuildErrors[0]
	assert.Equal(t, ErrDuplicateKey, err.Kind)
	assert.Equal(t, 2, err.NodeKey)
	assert.True(t, errors.Is(err, ErrKindDuplicateKey))
	assert.Len(t, handled, 1)

	children1, ok := tree.Children(1)
	assert.True(t, ok)
	assert.Equal(t, []int{2, 3}, keysOf(children1), "kept first occurrence must stay wired as child of 1")
}

func TestBuildDuplicateKeyKeepsFirstInStructure(t *testing.T) {
	// 回归：重复 key 的首现节点必须仍然接在树结构里，
	// 不能只存在于 NodeMap 而不可达（"幽灵节点"）。
	nodes := []*testNode{
		node(1, 0, true),
		node(2, 1, false), // 首现，被保留
		node(3, 1, false),
		node(2, 1, false), // 重复
	}

	tree := NewTreeBuilder[int, *testNode]().Build(nodes)
	assert.Len(t, tree.BuildErrors, 1)

	children1, ok := tree.Children(1)
	assert.True(t, ok)
	assert.Equal(t, []int{2, 3}, keysOf(children1))

	var walked []int
	tree.Walk(func(n *testNode, _ int) bool {
		walked = append(walked, n.GetKey())
		return true
	})
	assert.Equal(t, []int{1, 2, 3}, walked)
}

func TestBuildDuplicateRootKeepsFirstInStructure(t *testing.T) {
	// 回归：首现节点是 root 时，它仍必须出现在 Roots 中，
	// 且其他节点可以正常挂到它名下。
	first := node(2, 0, true)
	nodes := []*testNode{
		node(1, 0, true),
		first,
		node(3, 2, false),
		node(2, 0, true), // 重复的 root
	}

	tree := NewTreeBuilder[int, *testNode]().Build(nodes)
	assert.Len(t, tree.BuildErrors, 1)
	assert.Equal(t, first, tree.NodeMap[2])
	assert.Equal(t, []int{1, 2}, keysOf(tree.Roots))

	children2, ok := tree.Children(2)
	assert.True(t, ok)
	assert.Equal(t, []int{3}, keysOf(children2))
}

func TestBuildOrphanStrategies(t *testing.T) {
	tests := []struct {
		name         string
		strategy     OrphanStrategy
		wantRoots    []int
		wantErrKinds []ErrorKind
	}{
		{name: "ignore", strategy: IgnoreOrphans, wantRoots: []int{1}},
		{name: "collect", strategy: CollectOrphans, wantRoots: []int{1, 2}},
		{name: "error", strategy: ErrorOnOrphans, wantRoots: []int{1}, wantErrKinds: []ErrorKind{ErrOrphanNode}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var handled []*BuildError[int]
			b := NewTreeBuilder[int, *testNode](
				WithOrphanStrategy[int, *testNode](tt.strategy),
				WithErrorHandler[int, *testNode](func(_ context.Context, err *BuildError[int]) {
					handled = append(handled, err)
				}),
			)

			tree := b.Build([]*testNode{
				node(1, 0, true),
				node(2, 99, false),
			})

			assert.Equal(t, tt.wantRoots, keysOf(tree.Roots))
			assert.Len(t, tree.BuildErrors, len(tt.wantErrKinds))

			for i, kind := range tt.wantErrKinds {
				assert.Equal(t, kind, tree.BuildErrors[i].Kind)
			}

			assert.Len(t, handled, len(tt.wantErrKinds))
		})
	}
}

func TestBuildRemoveCycles(t *testing.T) {
	var handled []*BuildError[int]
	b := NewTreeBuilder[int, *testNode](WithErrorHandler[int, *testNode](func(_ context.Context, err *BuildError[int]) {
		handled = append(handled, err)
	}))

	// 1 -> 2 -> 3 -> 1
	tree := b.Build([]*testNode{
		node(1, 3, false),
		node(2, 1, false),
		node(3, 2, false),
	})

	if len(tree.BuildErrors) != 1 {
		t.Fatalf("BuildErrors len = %d, want 1", len(tree.BuildErrors))
	}
	err := tree.BuildErrors[0]
	if err.Kind != ErrCyclicGraph {
		t.Fatalf("error kind = %v, want %v", err.Kind, ErrCyclicGraph)
	}
	if !errors.Is(err, ErrKindCyclicGraph) {
		t.Fatalf("error should match ErrKindCyclicGraph")
	}
	if len(handled) != 1 {
		t.Fatalf("error handler called %d times, want 1", len(handled))
	}

	children3, ok := tree.Children(3)
	if !ok {
		t.Fatalf("Children(3) should exist")
	}
	if children3 != nil {
		t.Fatalf("Children(3) = %v, want nil after cycle edge removed", children3)
	}
}

func TestBuildSelfLoopRemovesEdge(t *testing.T) {
	// 节点 2 的父节点是它自己：自环应被检测并移除。
	tree := NewTreeBuilder[int, *testNode]().Build([]*testNode{
		node(1, 0, true),
		node(2, 2, false),
	})

	assert.Len(t, tree.BuildErrors, 1)
	assert.Equal(t, ErrCyclicGraph, tree.BuildErrors[0].Kind)
	assert.Equal(t, 2, tree.BuildErrors[0].NodeKey)

	children2, ok := tree.Children(2)
	assert.True(t, ok)
	assert.Nil(t, children2, "self edge should be removed")
}

func TestBuildMultipleDisjointCycles(t *testing.T) {
	// 环 A: 1->2->3->1；环 B: 4->5->4
	tree := NewTreeBuilder[int, *testNode]().Build([]*testNode{
		node(1, 3, false),
		node(2, 1, false),
		node(3, 2, false),
		node(4, 5, false),
		node(5, 4, false),
	})

	assert.Len(t, tree.BuildErrors, 2)

	c3, ok := tree.Children(3)
	assert.True(t, ok)
	assert.Nil(t, c3, "cycle A edge 3->1 removed")
	c5, ok := tree.Children(5)
	assert.True(t, ok)
	assert.Nil(t, c5, "cycle B edge 5->4 removed")

	// 环内其余边保留：1->2, 2->3, 4->5
	c1, _ := tree.Children(1)
	assert.Equal(t, []int{2}, keysOf(c1))
	c2, _ := tree.Children(2)
	assert.Equal(t, []int{3}, keysOf(c2))
	c4, _ := tree.Children(4)
	assert.Equal(t, []int{5}, keysOf(c4))
}

func TestBuildCycleAndRootComponentCoexist(t *testing.T) {
	// 根可达分量 0->1->2->3 不受孤立环 4->5->4 影响。
	tree := NewTreeBuilder[int, *testNode]().Build([]*testNode{
		node(0, -1, true),
		node(1, 0, false),
		node(2, 1, false),
		node(3, 2, false),
		node(4, 5, false),
		node(5, 4, false),
	})

	assert.Len(t, tree.BuildErrors, 1)
	assert.Equal(t, []int{0}, keysOf(tree.Roots))

	c0, _ := tree.Children(0)
	assert.Equal(t, []int{1}, keysOf(c0))
	c1, _ := tree.Children(1)
	assert.Equal(t, []int{2}, keysOf(c1))
	c2, _ := tree.Children(2)
	assert.Equal(t, []int{3}, keysOf(c2))

	c4, _ := tree.Children(4)
	assert.Equal(t, []int{5}, keysOf(c4))
	c5, ok := tree.Children(5)
	assert.True(t, ok)
	assert.Nil(t, c5, "cycle edge 5->4 removed")
}

func TestBuildContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var handled []*BuildError[int]
	b := NewTreeBuilder[int, *testNode](
		WithContext[int, *testNode](ctx),
		WithErrorHandler[int, *testNode](func(_ context.Context, err *BuildError[int]) {
			handled = append(handled, err)
		}),
	)

	tree := b.Build([]*testNode{
		node(1, 0, true),
		node(2, 1, false),
	})

	if len(tree.NodeMap) != 0 {
		t.Fatalf("NodeMap len = %d, want 0", len(tree.NodeMap))
	}
	if len(tree.BuildErrors) != 1 {
		t.Fatalf("BuildErrors len = %d, want 1", len(tree.BuildErrors))
	}
	err := tree.BuildErrors[0]
	if err.Kind != ErrContextDone {
		t.Fatalf("error kind = %v, want %v", err.Kind, ErrContextDone)
	}
	if !errors.Is(err, ErrKindContextDone) {
		t.Fatalf("error should match ErrKindContextDone")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error should preserve the real context error")
	}
	if len(handled) != 1 {
		t.Fatalf("error handler called %d times, want 1", len(handled))
	}
}

// flakyCtx 前 failAfter 次 Err() 调用返回 nil，之后返回 context.Canceled，
// 用于确定性测试构建中途取消。
type flakyCtx struct {
	context.Context
	failAfter int
	calls     int
}

func (c *flakyCtx) Err() error {
	c.calls++
	if c.calls > c.failAfter {
		return context.Canceled
	}
	return nil
}

// chainNodes 生成 1..n 的链：1 为根，i 的父节点为 i-1。
func chainNodes(n int) []*testNode {
	nodes := make([]*testNode, 0, n)
	for i := 1; i <= n; i++ {
		nodes = append(nodes, node(i, i-1, i == 1))
	}
	return nodes
}

func TestBuildContextCanceledMidBuild(t *testing.T) {
	// 前两遍共 10+10 次检查，第 3 阶段前 1 次检查，取消发生在 removeCycles 阶段。
	ctx := &flakyCtx{Context: context.Background(), failAfter: 22}
	tree := NewTreeBuilder[int, *testNode](
		WithContext[int, *testNode](ctx),
		WithComparator[int, *testNode](OrderComparator[*testNode, int]{}),
	).Build(chainNodes(10))

	// 索引与父子关系已建完，removeCycles 被中断。
	assert.Len(t, tree.NodeMap, 10)
	assert.Len(t, tree.BuildErrors, 1)
	err := tree.BuildErrors[0]
	assert.Equal(t, ErrContextDone, err.Kind)
	assert.True(t, errors.Is(err, ErrKindContextDone))
	assert.True(t, errors.Is(err, context.Canceled))
}

func TestBuildContextCanceledDuringSort(t *testing.T) {
	// 10+10+1(阶段3前)+20(removeCycles)+1(阶段4前)=42 次检查后，
	// 取消发生在 sortByLevel 阶段。
	ctx := &flakyCtx{Context: context.Background(), failAfter: 44}
	tree := NewTreeBuilder[int, *testNode](
		WithContext[int, *testNode](ctx),
		WithComparator[int, *testNode](OrderComparator[*testNode, int]{}),
	).Build(chainNodes(10))

	assert.Len(t, tree.BuildErrors, 1)
	assert.Equal(t, ErrContextDone, tree.BuildErrors[0].Kind)
}

func TestBuildNilOptionsDoNotPanic(t *testing.T) {
	tree := NewTreeBuilder[int, *testNode](
		WithContext[int, *testNode](nil),
		WithErrorHandler[int, *testNode](nil),
	).Build([]*testNode{node(1, 0, true)})

	assert.Equal(t, []int{1}, keysOf(tree.Roots))
	assert.Empty(t, tree.BuildErrors)
}

func TestBuildSortingWithComparator(t *testing.T) {
	r1 := node(1, 0, true)
	r1.sortOrder = 2
	r1.sortName = "b"

	r2 := node(2, 0, true)
	r2.sortOrder = 1
	r2.sortName = "a"

	c1 := node(3, 1, false)
	c1.sortOrder = 2
	c1.sortName = "b"

	c2 := node(4, 1, false)
	c2.sortOrder = 1
	c2.sortName = "a"

	tree := NewTreeBuilder[int, *testNode](
		WithComparator[int, *testNode](OrderComparator[*testNode, int]{}),
	).Build([]*testNode{r1, r2, c1, c2})

	assert.Equal(t, []int{2, 1}, keysOf(tree.Roots))
	children1, ok := tree.Children(1)
	assert.True(t, ok)
	assert.Equal(t, []int{4, 3}, keysOf(children1))
}

func TestComparators(t *testing.T) {
	a := &testNode{nodeKey: 1, sortID: 1, sortName: "a", sortOrder: 1}
	b := &testNode{nodeKey: 2, sortID: 2, sortName: "b", sortOrder: 2}
	c := &testNode{nodeKey: 3, sortID: 2, sortName: "c", sortOrder: 2}

	if got := (IDComparator[*testNode, int]{}).Compare(a, b); got != -1 {
		t.Fatalf("IDComparator(a,b) = %d, want -1", got)
	}
	if got := (IDComparator[*testNode, int]{}).Compare(b, a); got != 1 {
		t.Fatalf("IDComparator(b,a) = %d, want 1", got)
	}
	if got := (IDComparator[*testNode, int]{}).Compare(b, c); got != 0 {
		t.Fatalf("IDComparator(b,c) = %d, want 0", got)
	}

	if got := (NameComparator[*testNode, int]{}).Compare(a, b); got != -1 {
		t.Fatalf("NameComparator(a,b) = %d, want -1", got)
	}
	if got := (OrderComparator[*testNode, int]{}).Compare(b, c); got != 0 {
		t.Fatalf("OrderComparator(b,c) = %d, want 0", got)
	}

	comp := NewCompositeComparator[*testNode](
		OrderComparator[*testNode, int]{},
		NameComparator[*testNode, int]{},
	)
	if got := comp.Compare(b, c); got != -1 {
		t.Fatalf("CompositeComparator(b,c) = %d, want -1", got)
	}

	cmpFunc := ComparatorFunc[*testNode](func(x, y *testNode) int {
		switch {
		case x.nodeKey < y.nodeKey:
			return -1
		case x.nodeKey > y.nodeKey:
			return 1
		default:
			return 0
		}
	})
	if got := cmpFunc.Compare(a, b); got != -1 {
		t.Fatalf("ComparatorFunc(a,b) = %d, want -1", got)
	}
}

func TestBuildErrorAndErrorKind(t *testing.T) {
	err := newBuildError[int](ErrOrphanNode, 10, 99)

	if err.Kind.String() != "orphan node" {
		t.Fatalf("ErrOrphanNode.String() = %q, want %q", err.Kind.String(), "orphan node")
	}
	if !errors.Is(err, ErrKindOrphanNode) {
		t.Fatalf("build error should unwrap to ErrKindOrphanNode")
	}
	if got := err.Error(); got == "" {
		t.Fatalf("BuildError.Error() should not be empty")
	}
}
