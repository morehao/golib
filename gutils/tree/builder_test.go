package tree

import (
	"testing"
)

// ============ 测试用的简单节点结构 ============

type SimpleNode struct {
	ID       string
	ParentID string
	Name     string
	Order    int
	children []TreeNode[string]
}

func (n *SimpleNode) GetKey() string                          { return n.ID }
func (n *SimpleNode) GetParentKey() string                    { return n.ParentID }
func (n *SimpleNode) IsRoot() bool                            { return n.ParentID == "" }
func (n *SimpleNode) GetChildren() []TreeNode[string]         { return n.children }
func (n *SimpleNode) SetChildren(children []TreeNode[string]) { n.children = children }
func (n *SimpleNode) GetID() uint                             { return uint(n.Order) }
func (n *SimpleNode) GetName() string                         { return n.Name }

// ============ 辅助测试函数 ============

func assertEq(t *testing.T, expected, actual interface{}, msg string) {
	t.Helper()
	if expected != actual {
		t.Errorf("%s: expected %v, got %v", msg, expected, actual)
	}
}

func assertLen(t *testing.T, slice interface{}, expected int, msg string) {
	t.Helper()
	var actual int
	switch v := slice.(type) {
	case []*SimpleNode:
		actual = len(v)
	case []TreeNode[string]:
		actual = len(v)
	default:
		t.Errorf("assertLen: unsupported type %T", slice)
		return
	}
	if actual != expected {
		t.Errorf("%s: expected length %d, got %d", msg, expected, actual)
	}
}

func assertTrue(t *testing.T, condition bool, msg string) {
	t.Helper()
	if !condition {
		t.Errorf("%s: condition is false", msg)
	}
}

// ============ 基础功能测试 ============

func TestTreeBuilder_EmptyNodes(t *testing.T) {
	builder := NewTreeBuilder[string, *SimpleNode]()
	result := builder.Build([]*SimpleNode{})

	assertLen(t, result, 0, "Empty input should return empty result")
}

func TestTreeBuilder_SingleRoot(t *testing.T) {
	nodes := []*SimpleNode{
		{ID: "1", ParentID: "", Name: "Root", Order: 1},
	}

	builder := NewTreeBuilder[string, *SimpleNode]()
	result := builder.Build(nodes)

	assertLen(t, result, 1, "Should have one root")
	assertEq(t, "1", result[0].ID, "Root ID")
	assertEq(t, "Root", result[0].Name, "Root name")
	assertLen(t, result[0].GetChildren(), 0, "Root should have no children")
}

func TestTreeBuilder_SimpleTree(t *testing.T) {
	nodes := []*SimpleNode{
		{ID: "1", ParentID: "", Name: "Root", Order: 1},
		{ID: "2", ParentID: "1", Name: "Child1", Order: 2},
		{ID: "3", ParentID: "1", Name: "Child2", Order: 3},
	}

	builder := NewTreeBuilder[string, *SimpleNode]()
	result := builder.Build(nodes)

	assertLen(t, result, 1, "Should have one root")
	assertEq(t, "1", result[0].ID, "Root ID")

	children := result[0].GetChildren()
	assertLen(t, children, 2, "Root should have two children")
	assertEq(t, "2", children[0].(*SimpleNode).ID, "First child ID")
	assertEq(t, "3", children[1].(*SimpleNode).ID, "Second child ID")
}

func TestTreeBuilder_MultipleRoots(t *testing.T) {
	nodes := []*SimpleNode{
		{ID: "1", ParentID: "", Name: "Root1", Order: 1},
		{ID: "2", ParentID: "", Name: "Root2", Order: 2},
		{ID: "3", ParentID: "1", Name: "Child1", Order: 3},
		{ID: "4", ParentID: "2", Name: "Child2", Order: 4},
	}

	builder := NewTreeBuilder[string, *SimpleNode]()
	result := builder.Build(nodes)

	assertLen(t, result, 2, "Should have two roots")
	assertEq(t, "1", result[0].ID, "First root ID")
	assertEq(t, "2", result[1].ID, "Second root ID")

	// 验证子节点
	assertLen(t, result[0].GetChildren(), 1, "First root should have one child")
	assertLen(t, result[1].GetChildren(), 1, "Second root should have one child")
}

func TestTreeBuilder_DeepTree(t *testing.T) {
	nodes := []*SimpleNode{
		{ID: "1", ParentID: "", Name: "Root", Order: 1},
		{ID: "2", ParentID: "1", Name: "Level1", Order: 2},
		{ID: "3", ParentID: "2", Name: "Level2", Order: 3},
		{ID: "4", ParentID: "3", Name: "Level3", Order: 4},
	}

	builder := NewTreeBuilder[string, *SimpleNode]()
	result := builder.Build(nodes)

	assertLen(t, result, 1, "Should have one root")

	// 验证树的深度
	level1 := result[0].GetChildren()
	assertLen(t, level1, 1, "Level 1 should have one child")

	level2 := level1[0].(*SimpleNode).GetChildren()
	assertLen(t, level2, 1, "Level 2 should have one child")

	level3 := level2[0].(*SimpleNode).GetChildren()
	assertLen(t, level3, 1, "Level 3 should have one child")

	assertEq(t, "4", level3[0].(*SimpleNode).ID, "Deepest node ID")
}

func TestTreeBuilder_ComplexTree(t *testing.T) {
	nodes := []*SimpleNode{
		{ID: "1", ParentID: "", Name: "Root", Order: 1},
		{ID: "2", ParentID: "1", Name: "Child1", Order: 2},
		{ID: "3", ParentID: "1", Name: "Child2", Order: 3},
		{ID: "4", ParentID: "2", Name: "GrandChild1", Order: 4},
		{ID: "5", ParentID: "2", Name: "GrandChild2", Order: 5},
		{ID: "6", ParentID: "3", Name: "GrandChild3", Order: 6},
	}

	builder := NewTreeBuilder[string, *SimpleNode]()
	result := builder.Build(nodes)

	assertLen(t, result, 1, "Should have one root")
	root := result[0]

	// 验证第一层子节点
	children := root.GetChildren()
	assertLen(t, children, 2, "Root should have two children")

	// 验证第二层子节点
	child1Children := children[0].(*SimpleNode).GetChildren()
	assertLen(t, child1Children, 2, "Child1 should have two children")

	child2Children := children[1].(*SimpleNode).GetChildren()
	assertLen(t, child2Children, 1, "Child2 should have one child")
}

// ============ 排序功能测试 ============

type SimpleNodeOrderComparator struct{}

func (c SimpleNodeOrderComparator) Compare(a, b *SimpleNode) int {
	if a.Order < b.Order {
		return -1
	} else if a.Order > b.Order {
		return 1
	}
	return 0
}

func TestTreeBuilder_WithOrderComparator(t *testing.T) {
	nodes := []*SimpleNode{
		{ID: "1", ParentID: "", Name: "Root", Order: 3},
		{ID: "2", ParentID: "1", Name: "Child3", Order: 30},
		{ID: "3", ParentID: "1", Name: "Child1", Order: 10},
		{ID: "4", ParentID: "1", Name: "Child2", Order: 20},
	}

	builder := NewTreeBuilder[string, *SimpleNode](
		WithComparator[string](SimpleNodeOrderComparator{}),
	)
	result := builder.Build(nodes)

	assertLen(t, result, 1, "Should have one root")
	children := result[0].GetChildren()
	assertLen(t, children, 3, "Should have three children")

	// 验证按 Order 排序
	assertEq(t, 10, children[0].(*SimpleNode).Order, "First child order")
	assertEq(t, 20, children[1].(*SimpleNode).Order, "Second child order")
	assertEq(t, 30, children[2].(*SimpleNode).Order, "Third child order")
}

func TestTreeBuilder_WithIDComparator(t *testing.T) {
	nodes := []*SimpleNode{
		{ID: "1", ParentID: "", Name: "Root", Order: 3},
		{ID: "2", ParentID: "1", Name: "Child3", Order: 30},
		{ID: "3", ParentID: "1", Name: "Child1", Order: 10},
		{ID: "4", ParentID: "1", Name: "Child2", Order: 20},
	}

	builder := NewTreeBuilder[string, *SimpleNode](
		WithComparator[string](IDComparator[*SimpleNode]{}),
	)
	result := builder.Build(nodes)

	assertLen(t, result, 1, "Should have one root")
	children := result[0].GetChildren()
	assertLen(t, children, 3, "Should have three children")

	// 验证按 Order (GetID返回Order) 排序
	assertEq(t, uint(10), children[0].(*SimpleNode).GetID(), "First child ID")
	assertEq(t, uint(20), children[1].(*SimpleNode).GetID(), "Second child ID")
	assertEq(t, uint(30), children[2].(*SimpleNode).GetID(), "Third child ID")
}

func TestTreeBuilder_WithNameComparator(t *testing.T) {
	nodes := []*SimpleNode{
		{ID: "1", ParentID: "", Name: "Root", Order: 1},
		{ID: "2", ParentID: "1", Name: "Charlie", Order: 2},
		{ID: "3", ParentID: "1", Name: "Alice", Order: 3},
		{ID: "4", ParentID: "1", Name: "Bob", Order: 4},
	}

	builder := NewTreeBuilder[string, *SimpleNode](
		WithComparator[string](NameComparator[*SimpleNode]{}),
	)
	result := builder.Build(nodes)

	assertLen(t, result, 1, "Should have one root")
	children := result[0].GetChildren()
	assertLen(t, children, 3, "Should have three children")

	// 验证按名称字母序排序
	assertEq(t, "Alice", children[0].(*SimpleNode).Name, "First child name")
	assertEq(t, "Bob", children[1].(*SimpleNode).Name, "Second child name")
	assertEq(t, "Charlie", children[2].(*SimpleNode).Name, "Third child name")
}

func TestTreeBuilder_RecursiveSorting(t *testing.T) {
	nodes := []*SimpleNode{
		{ID: "1", ParentID: "", Name: "Root", Order: 1},
		{ID: "2", ParentID: "1", Name: "Child2", Order: 20},
		{ID: "3", ParentID: "1", Name: "Child1", Order: 10},
		{ID: "4", ParentID: "2", Name: "GrandChild2", Order: 202},
		{ID: "5", ParentID: "2", Name: "GrandChild1", Order: 201},
	}

	builder := NewTreeBuilder[string, *SimpleNode](
		WithComparator[string](SimpleNodeOrderComparator{}),
	)
	result := builder.Build(nodes)

	assertLen(t, result, 1, "Should have one root")

	// 验证第一层排序
	children := result[0].GetChildren()
	assertLen(t, children, 2, "Should have two children")
	assertEq(t, 10, children[0].(*SimpleNode).Order, "First child order")
	assertEq(t, 20, children[1].(*SimpleNode).Order, "Second child order")

	// 验证第二层排序
	grandChildren := children[1].(*SimpleNode).GetChildren()
	assertLen(t, grandChildren, 2, "Should have two grandchildren")
	assertEq(t, 201, grandChildren[0].(*SimpleNode).Order, "First grandchild order")
	assertEq(t, 202, grandChildren[1].(*SimpleNode).Order, "Second grandchild order")
}

func TestTreeBuilder_CompositeComparator(t *testing.T) {
	nodes := []*SimpleNode{
		{ID: "1", ParentID: "", Name: "Root", Order: 1},
		{ID: "2", ParentID: "1", Name: "Bob", Order: 20},
		{ID: "3", ParentID: "1", Name: "Alice", Order: 20},
		{ID: "4", ParentID: "1", Name: "Charlie", Order: 10},
	}

	// 先按 Order，再按 Name
	compositeComp := NewCompositeComparator[*SimpleNode](
		SimpleNodeOrderComparator{},
		NameComparator[*SimpleNode]{},
	)

	builder := NewTreeBuilder[string, *SimpleNode](
		WithComparator[string](compositeComp),
	)
	result := builder.Build(nodes)

	assertLen(t, result, 1, "Should have one root")
	children := result[0].GetChildren()
	assertLen(t, children, 3, "Should have three children")

	// Charlie (Order=10) 应该排在最前面
	assertEq(t, "Charlie", children[0].(*SimpleNode).Name, "First child name")

	// Alice 和 Bob 的 Order 相同(20)，按名称排序
	assertEq(t, "Alice", children[1].(*SimpleNode).Name, "Second child name")
	assertEq(t, "Bob", children[2].(*SimpleNode).Name, "Third child name")
}

// ============ 错误处理测试 ============

func TestTreeBuilder_MissingParent(t *testing.T) {
	nodes := []*SimpleNode{
		{ID: "1", ParentID: "", Name: "Root", Order: 1},
		{ID: "2", ParentID: "999", Name: "Orphan", Order: 2}, // 父节点不存在
	}

	var capturedNodeKey, capturedParentKey string
	errorHandlerCalled := false

	builder := NewTreeBuilder[string, *SimpleNode]()

	result := builder.Build(nodes)

	// 只有 Root 节点被添加到树中
	assertLen(t, result, 1, "Should have one root")
	assertEq(t, "1", result[0].ID, "Root ID")

	// 验证错误处理函数被调用
	assertTrue(t, errorHandlerCalled, "Error handler should be called")
	assertEq(t, "2", capturedNodeKey, "Captured node key")
	assertEq(t, "999", capturedParentKey, "Captured parent key")
}

func TestTreeBuilder_MultipleOrphans(t *testing.T) {
	nodes := []*SimpleNode{
		{ID: "1", ParentID: "", Name: "Root", Order: 1},
		{ID: "2", ParentID: "999", Name: "Orphan1", Order: 2},
		{ID: "3", ParentID: "888", Name: "Orphan2", Order: 3},
	}

	errorCount := 0
	builder := NewTreeBuilder[string, *SimpleNode]()

	result := builder.Build(nodes)

	assertLen(t, result, 1, "Should have one root")
	assertEq(t, 2, errorCount, "Error handler should be called twice")
}

// ============ 边界情况测试 ============

func TestTreeBuilder_AllRoots(t *testing.T) {
	nodes := []*SimpleNode{
		{ID: "1", ParentID: "", Name: "Root1", Order: 3},
		{ID: "2", ParentID: "", Name: "Root2", Order: 1},
		{ID: "3", ParentID: "", Name: "Root3", Order: 2},
	}

	builder := NewTreeBuilder[string, *SimpleNode](
		WithComparator[string](SimpleNodeOrderComparator{}),
	)
	result := builder.Build(nodes)

	assertLen(t, result, 3, "Should have three roots")
	// 验证排序
	assertEq(t, 1, result[0].Order, "First root order")
	assertEq(t, 2, result[1].Order, "Second root order")
	assertEq(t, 3, result[2].Order, "Third root order")
}

func TestTreeBuilder_NoRoots(t *testing.T) {
	nodes := []*SimpleNode{
		{ID: "1", ParentID: "999", Name: "Child1", Order: 1},
		{ID: "2", ParentID: "999", Name: "Child2", Order: 2},
	}

	builder := NewTreeBuilder[string, *SimpleNode]()
	result := builder.Build(nodes)

	// 所有节点都找不到父节点
	assertLen(t, result, 0, "Should have no roots")
}

func TestTreeBuilder_DuplicateKeys(t *testing.T) {
	nodes := []*SimpleNode{
		{ID: "1", ParentID: "", Name: "Root", Order: 1},
		{ID: "2", ParentID: "1", Name: "Child1", Order: 2},
		{ID: "2", ParentID: "1", Name: "Child2", Order: 3}, // 重复的 ID
	}

	builder := NewTreeBuilder[string, *SimpleNode]()
	result := builder.Build(nodes)

	assertLen(t, result, 1, "Should have one root")
	children := result[0].GetChildren()
	// 后面的节点会覆盖前面的节点
	assertLen(t, children, 1, "Should have one child")
	assertEq(t, "Child2", children[0].(*SimpleNode).Name, "Should keep the last duplicate")
}

// ============ 整数类型 Key 测试 ============

type IntKeyNode struct {
	ID       int64
	ParentID int64
	Name     string
	children []TreeNode[int64]
}

func (n *IntKeyNode) GetKey() int64                          { return n.ID }
func (n *IntKeyNode) GetParentKey() int64                    { return n.ParentID }
func (n *IntKeyNode) IsRoot() bool                           { return n.ParentID == 0 }
func (n *IntKeyNode) GetChildren() []TreeNode[int64]         { return n.children }
func (n *IntKeyNode) SetChildren(children []TreeNode[int64]) { n.children = children }
func (n *IntKeyNode) GetID() uint                            { return uint(n.ID) }

func TestTreeBuilder_IntegerKeys(t *testing.T) {
	nodes := []*IntKeyNode{
		{ID: 1, ParentID: 0, Name: "Root"},
		{ID: 2, ParentID: 1, Name: "Child1"},
		{ID: 3, ParentID: 1, Name: "Child2"},
	}

	builder := NewTreeBuilder[int64, *IntKeyNode]()
	result := builder.Build(nodes)

	assertLen(t, result, 1, "Should have one root")
	assertEq(t, int64(1), result[0].ID, "Root ID")
	assertLen(t, result[0].GetChildren(), 2, "Root should have two children")
}

// ============ 实际场景测试 ============

func TestTreeBuilder_FileSystem(t *testing.T) {
	nodes := []*SimpleNode{
		{ID: "root", ParentID: "", Name: "/", Order: 1},
		{ID: "usr", ParentID: "root", Name: "usr", Order: 2},
		{ID: "etc", ParentID: "root", Name: "etc", Order: 3},
		{ID: "bin", ParentID: "usr", Name: "bin", Order: 4},
		{ID: "bash", ParentID: "bin", Name: "bash", Order: 5},
	}

	builder := NewTreeBuilder[string, *SimpleNode]()
	result := builder.Build(nodes)

	assertLen(t, result, 1, "Should have one root")
	assertEq(t, "/", result[0].Name, "Root name")

	// 验证 /usr 和 /etc
	children := result[0].GetChildren()
	assertLen(t, children, 2, "Should have two directories")

	// 验证 /usr/bin
	usr := children[0].(*SimpleNode)
	assertEq(t, "usr", usr.Name, "usr directory")
	usrChildren := usr.GetChildren()
	assertLen(t, usrChildren, 1, "/usr should have one child")

	bin := usrChildren[0].(*SimpleNode)
	assertEq(t, "bin", bin.Name, "bin directory")

	// 验证 /usr/bin/bash
	binChildren := bin.GetChildren()
	assertLen(t, binChildren, 1, "/usr/bin should have one file")
	assertEq(t, "bash", binChildren[0].(*SimpleNode).Name, "bash file")
}

func TestTreeBuilder_Organization(t *testing.T) {
	nodes := []*SimpleNode{
		{ID: "1", ParentID: "", Name: "CEO", Order: 1},
		{ID: "2", ParentID: "1", Name: "CTO", Order: 2},
		{ID: "3", ParentID: "1", Name: "CFO", Order: 3},
		{ID: "4", ParentID: "2", Name: "Dev Team", Order: 4},
		{ID: "5", ParentID: "2", Name: "QA Team", Order: 5},
		{ID: "6", ParentID: "3", Name: "Accounting", Order: 6},
	}

	builder := NewTreeBuilder[string, *SimpleNode](
		WithComparator[string](SimpleNodeOrderComparator{}),
	)
	result := builder.Build(nodes)

	assertLen(t, result, 1, "Should have one root")
	ceo := result[0]
	assertEq(t, "CEO", ceo.Name, "CEO name")

	// 验证 CTO 和 CFO
	executives := ceo.GetChildren()
	assertLen(t, executives, 2, "CEO should have two direct reports")

	cto := executives[0].(*SimpleNode)
	assertEq(t, "CTO", cto.Name, "CTO name")

	// 验证 Dev Team 和 QA Team
	ctoTeams := cto.GetChildren()
	assertLen(t, ctoTeams, 2, "CTO should have two teams")
	assertEq(t, "Dev Team", ctoTeams[0].(*SimpleNode).Name, "First team")
	assertEq(t, "QA Team", ctoTeams[1].(*SimpleNode).Name, "Second team")
}

// ============ Benchmark 测试 ============

func BenchmarkTreeBuilder_Small(b *testing.B) {
	nodes := []*SimpleNode{
		{ID: "1", ParentID: "", Name: "Root", Order: 1},
		{ID: "2", ParentID: "1", Name: "Child1", Order: 2},
		{ID: "3", ParentID: "1", Name: "Child2", Order: 3},
		{ID: "4", ParentID: "2", Name: "GrandChild1", Order: 4},
	}

	builder := NewTreeBuilder[string, *SimpleNode](
		WithComparator[string](SimpleNodeOrderComparator{}),
	)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = builder.Build(nodes)
	}
}

func BenchmarkTreeBuilder_Medium(b *testing.B) {
	nodes := make([]*SimpleNode, 100)
	nodes[0] = &SimpleNode{ID: "0", ParentID: "", Name: "Root", Order: 0}

	for i := 1; i < 100; i++ {
		parentID := "0"
		if i > 10 {
			parentID = nodes[(i-1)/10].ID
		}
		nodes[i] = &SimpleNode{
			ID:       string(rune('0' + i)),
			ParentID: parentID,
			Name:     "Node",
			Order:    i,
		}
	}

	builder := NewTreeBuilder[string, *SimpleNode](
		WithComparator[string](SimpleNodeOrderComparator{}),
	)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = builder.Build(nodes)
	}
}

func BenchmarkTreeBuilder_WithoutSort(b *testing.B) {
	nodes := make([]*SimpleNode, 100)
	nodes[0] = &SimpleNode{ID: "0", ParentID: "", Name: "Root", Order: 0}

	for i := 1; i < 100; i++ {
		parentID := "0"
		if i > 10 {
			parentID = nodes[(i-1)/10].ID
		}
		nodes[i] = &SimpleNode{
			ID:       string(rune('0' + i)),
			ParentID: parentID,
			Name:     "Node",
			Order:    i,
		}
	}

	builder := NewTreeBuilder[string, *SimpleNode]()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = builder.Build(nodes)
	}
}
