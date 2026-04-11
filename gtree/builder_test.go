package gtree

import (
	"context"
	"errors"
	"reflect"
	"testing"
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

func TestBuildBasicTreeAndQueries(t *testing.T) {
	nodes := []*testNode{
		node(1, 0, true),
		node(2, 1, false),
		node(3, 1, false),
		node(4, 2, false),
	}

	tree := NewTreeBuilder[int, *testNode]().Build(nodes)

	if got := keysOf(tree.Roots); !reflect.DeepEqual(got, []int{1}) {
		t.Fatalf("roots = %v, want [1]", got)
	}

	children1, ok := tree.Children(1)
	if !ok {
		t.Fatalf("Children(1) should exist")
	}
	if got := keysOf(children1); !reflect.DeepEqual(got, []int{2, 3}) {
		t.Fatalf("Children(1) = %v, want [2 3]", got)
	}

	children3, ok := tree.Children(3)
	if !ok {
		t.Fatalf("Children(3) should exist")
	}
	if children3 != nil {
		t.Fatalf("Children(3) = %v, want nil for leaf", children3)
	}

	if childrenX, ok := tree.Children(999); ok || childrenX != nil {
		t.Fatalf("Children(999) = (%v, %v), want (nil, false)", childrenX, ok)
	}

	levels := tree.GetNodesByLevel()
	if got := keysOf(levels[0]); !reflect.DeepEqual(got, []int{1}) {
		t.Fatalf("level 0 = %v, want [1]", got)
	}
	if got := keysOf(levels[1]); !reflect.DeepEqual(got, []int{2, 3}) {
		t.Fatalf("level 1 = %v, want [2 3]", got)
	}
	if got := keysOf(levels[2]); !reflect.DeepEqual(got, []int{4}) {
		t.Fatalf("level 2 = %v, want [4]", got)
	}

	if max := tree.MaxLevel(); max != 2 {
		t.Fatalf("MaxLevel = %d, want 2", max)
	}

	var walked []int
	tree.Walk(func(n *testNode, level int) bool {
		_ = level
		walked = append(walked, n.GetKey())
		return true
	})
	if !reflect.DeepEqual(walked, []int{1, 2, 4, 3}) {
		t.Fatalf("Walk order = %v, want [1 2 4 3]", walked)
	}

	var early []int
	tree.Walk(func(n *testNode, level int) bool {
		_ = level
		early = append(early, n.GetKey())
		return len(early) < 2
	})
	if !reflect.DeepEqual(early, []int{1, 2}) {
		t.Fatalf("Walk early stop = %v, want [1 2]", early)
	}

	filtered := tree.Filter(func(n *testNode) bool { return n.GetKey()%2 == 0 })
	if got := keysOf(filtered); !reflect.DeepEqual(got, []int{2, 4}) {
		t.Fatalf("Filter even keys = %v, want [2 4]", got)
	}
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

	if tree.NodeMap[2] != first {
		t.Fatalf("NodeMap[2] should keep first node")
	}

	if len(tree.BuildErrors) != 1 {
		t.Fatalf("BuildErrors len = %d, want 1", len(tree.BuildErrors))
	}
	err := tree.BuildErrors[0]
	if err.Kind != ErrDuplicateKey {
		t.Fatalf("error kind = %v, want %v", err.Kind, ErrDuplicateKey)
	}
	if err.NodeKey != 2 {
		t.Fatalf("error node key = %d, want 2", err.NodeKey)
	}
	if !errors.Is(err, ErrKindDuplicateKey) {
		t.Fatalf("error should match ErrKindDuplicateKey")
	}
	if len(handled) != 1 {
		t.Fatalf("error handler called %d times, want 1", len(handled))
	}

	children1, ok := tree.Children(1)
	if !ok {
		t.Fatalf("Children(1) should exist")
	}
	if got := keysOf(children1); !reflect.DeepEqual(got, []int{3}) {
		t.Fatalf("Children(1) = %v, want [3]", got)
	}
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

			if got := keysOf(tree.Roots); !reflect.DeepEqual(got, tt.wantRoots) {
				t.Fatalf("roots = %v, want %v", got, tt.wantRoots)
			}

			if len(tree.BuildErrors) != len(tt.wantErrKinds) {
				t.Fatalf("BuildErrors len = %d, want %d", len(tree.BuildErrors), len(tt.wantErrKinds))
			}
			for i, kind := range tt.wantErrKinds {
				if tree.BuildErrors[i].Kind != kind {
					t.Fatalf("BuildErrors[%d].Kind = %v, want %v", i, tree.BuildErrors[i].Kind, kind)
				}
			}

			if len(handled) != len(tt.wantErrKinds) {
				t.Fatalf("handler called %d times, want %d", len(handled), len(tt.wantErrKinds))
			}
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
	if len(handled) != 1 {
		t.Fatalf("error handler called %d times, want 1", len(handled))
	}
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

	if got := keysOf(tree.Roots); !reflect.DeepEqual(got, []int{2, 1}) {
		t.Fatalf("sorted roots = %v, want [2 1]", got)
	}
	children1, ok := tree.Children(1)
	if !ok {
		t.Fatalf("Children(1) should exist")
	}
	if got := keysOf(children1); !reflect.DeepEqual(got, []int{4, 3}) {
		t.Fatalf("sorted children(1) = %v, want [4 3]", got)
	}
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
