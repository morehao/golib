package gtree

import (
	"context"
	"fmt"
	"testing"
)

// TestNode 测试用的节点结构
type TestNode struct {
	ID       uint
	ParentID uint
	Name     string
	Order    int
	Children []TreeNode[uint]
}

func (n *TestNode) GetKey() uint                          { return n.ID }
func (n *TestNode) GetParentID() uint                     { return n.ParentID }
func (n *TestNode) GetParentKey() uint                    { return n.ParentID }
func (n *TestNode) SetChildren(children []TreeNode[uint]) { n.Children = children }
func (n *TestNode) GetChildren() []TreeNode[uint]         { return n.Children }
func (n *TestNode) IsRoot() bool                          { return n.ParentID == 0 }
func (n *TestNode) GetID() uint                           { return n.ID }
func (n *TestNode) GetName() string                       { return n.Name }
func (n *TestNode) GetOrder() int                         { return n.Order }

// 辅助函数：打印层级结构
func printLevelNodes(levelMap map[int][]*TestNode) {
	maxLevel := -1
	for level := range levelMap {
		if level > maxLevel {
			maxLevel = level
		}
	}

	for i := 0; i <= maxLevel; i++ {
		nodes := levelMap[i]
		fmt.Printf("Level %d: ", i)
		for j, node := range nodes {
			if j > 0 {
				fmt.Print(", ")
			}
			fmt.Printf("%s(ID:%d)", node.Name, node.ID)
		}
		fmt.Println()
	}
}

// TestBasicTreeBuild 测试基本的树构建
func TestBasicTreeBuild(t *testing.T) {
	nodes := []*TestNode{
		{ID: 1, ParentID: 0, Name: "Root", Order: 1},
		{ID: 2, ParentID: 1, Name: "Child1", Order: 2},
		{ID: 3, ParentID: 1, Name: "Child2", Order: 1},
		{ID: 4, ParentID: 2, Name: "GrandChild1", Order: 1},
		{ID: 5, ParentID: 2, Name: "GrandChild2", Order: 2},
	}

	builder := NewTreeBuilder[uint, *TestNode]()
	roots := builder.Build(nodes)

	if len(roots) != 1 {
		t.Errorf("Expected 1 root, got %d", len(roots))
	}

	// 测试层级输出
	levelMap := builder.GetNodesByLevel(roots)

	fmt.Println("\n=== Basic Tree Structure ===")
	printLevelNodes(levelMap)

	// 验证层级
	if len(levelMap[0]) != 1 {
		t.Errorf("Level 0: expected 1 node, got %d", len(levelMap[0]))
	}
	if len(levelMap[1]) != 2 {
		t.Errorf("Level 1: expected 2 nodes, got %d", len(levelMap[1]))
	}
	if len(levelMap[2]) != 2 {
		t.Errorf("Level 2: expected 2 nodes, got %d", len(levelMap[2]))
	}

	// 验证最大层级
	maxLevel := builder.GetMaxLevel(roots)
	if maxLevel != 2 {
		t.Errorf("Expected max level 2, got %d", maxLevel)
	}
}

// TestTreeWithSorting 测试带排序的树构建
func TestTreeWithSorting(t *testing.T) {
	nodes := []*TestNode{
		{ID: 1, ParentID: 0, Name: "Root", Order: 1},
		{ID: 2, ParentID: 1, Name: "Child1", Order: 3},
		{ID: 3, ParentID: 1, Name: "Child2", Order: 1},
		{ID: 4, ParentID: 1, Name: "Child3", Order: 2},
	}

	// 使用 Order 排序
	builder := NewTreeBuilder(
		WithComparator(OrderComparator[*TestNode, uint]{}),
	)
	roots := builder.Build(nodes)

	fmt.Println("\n=== Tree with Order Sorting ===")
	levelMap := builder.GetNodesByLevel(roots)
	printLevelNodes(levelMap)

	// 验证排序顺序
	children := roots[0].GetChildren()
	if len(children) != 3 {
		t.Errorf("Expected 3 children, got %d", len(children))
	}

	// 验证是否按 Order 排序
	child1 := children[0].(*TestNode)
	child2 := children[1].(*TestNode)
	child3 := children[2].(*TestNode)

	if child1.Order != 1 || child2.Order != 2 || child3.Order != 3 {
		t.Errorf("Children not sorted by order: %d, %d, %d", child1.Order, child2.Order, child3.Order)
	}
}

// TestMultipleRoots 测试多根节点
func TestMultipleRoots(t *testing.T) {
	nodes := []*TestNode{
		{ID: 1, ParentID: 0, Name: "Root1", Order: 2},
		{ID: 2, ParentID: 0, Name: "Root2", Order: 1},
		{ID: 3, ParentID: 1, Name: "Child1-1", Order: 1},
		{ID: 4, ParentID: 2, Name: "Child2-1", Order: 1},
	}

	builder := NewTreeBuilder(WithComparator(OrderComparator[*TestNode, uint]{}))
	roots := builder.Build(nodes)

	fmt.Println("\n=== Multiple Roots ===")
	levelMap := builder.GetNodesByLevel(roots)
	printLevelNodes(levelMap)

	if len(roots) != 2 {
		t.Errorf("Expected 2 roots, got %d", len(roots))
	}

	// 验证根节点排序
	if roots[0].GetOrder() != 1 || roots[1].GetOrder() != 2 {
		t.Errorf("Roots not sorted correctly")
	}
}

// TestOrphanNodesIgnore 测试孤儿节点（忽略策略）
func TestOrphanNodesIgnore(t *testing.T) {
	nodes := []*TestNode{
		{ID: 1, ParentID: 0, Name: "Root", Order: 1},
		{ID: 2, ParentID: 1, Name: "Child1", Order: 1},
		{ID: 3, ParentID: 999, Name: "Orphan", Order: 1}, // 孤儿节点
	}

	errorCalled := false
	builder := NewTreeBuilder(
		WithOrphanStrategy[uint, *TestNode](IgnoreOrphans),
		WithErrorHandler[uint, *TestNode](func(ctx context.Context, nodeKey, parentKey uint, err error) {
			errorCalled = true
			fmt.Printf("Error handler called for node %d, parent %d\n", nodeKey, parentKey)
		}),
	)

	roots := builder.Build(nodes)

	fmt.Println("\n=== Orphan Nodes (Ignore) ===")
	levelMap := builder.GetNodesByLevel(roots)
	printLevelNodes(levelMap)

	if !errorCalled {
		t.Error("Error handler should have been called for orphan node")
	}

	if len(roots) != 1 {
		t.Errorf("Expected 1 root (orphan ignored), got %d", len(roots))
	}
}

// TestOrphanNodesCollect 测试孤儿节点（收集策略）
func TestOrphanNodesCollect(t *testing.T) {
	nodes := []*TestNode{
		{ID: 1, ParentID: 0, Name: "Root", Order: 1},
		{ID: 2, ParentID: 1, Name: "Child1", Order: 1},
		{ID: 3, ParentID: 999, Name: "Orphan", Order: 2}, // 孤儿节点
	}

	builder := NewTreeBuilder(
		WithOrphanStrategy[uint, *TestNode](CollectOrphans),
	)

	roots := builder.Build(nodes)

	fmt.Println("\n=== Orphan Nodes (Collect) ===")
	levelMap := builder.GetNodesByLevel(roots)
	printLevelNodes(levelMap)

	if len(roots) != 2 {
		t.Errorf("Expected 2 roots (including orphan), got %d", len(roots))
	}
}

// TestCompositeComparator 测试组合比较器
func TestCompositeComparator(t *testing.T) {
	nodes := []*TestNode{
		{ID: 1, ParentID: 0, Name: "Root", Order: 1},
		{ID: 2, ParentID: 1, Name: "B", Order: 1},
		{ID: 3, ParentID: 1, Name: "A", Order: 1},
		{ID: 4, ParentID: 1, Name: "C", Order: 2},
	}

	// 先按 Order 排序，再按 Name 排序
	compositeComp := NewCompositeComparator[*TestNode, uint](
		OrderComparator[*TestNode, uint]{},
		NameComparator[*TestNode, uint]{},
	)

	builder := NewTreeBuilder(
		WithComparator(compositeComp),
	)

	roots := builder.Build(nodes)

	fmt.Println("\n=== Composite Comparator (Order then Name) ===")
	levelMap := builder.GetNodesByLevel(roots)
	printLevelNodes(levelMap)

	children := roots[0].GetChildren()
	// Order=1 的节点应该按 Name 排序：A, B
	// Order=2 的节点：C
	child1 := children[0].(*TestNode)
	child2 := children[1].(*TestNode)
	child3 := children[2].(*TestNode)

	if child1.Name != "A" || child2.Name != "B" || child3.Name != "C" {
		t.Errorf("Expected order A, B, C, got %s, %s, %s", child1.Name, child2.Name, child3.Name)
	}
}

// TestDeepTree 测试深层树结构
func TestDeepTree(t *testing.T) {
	nodes := []*TestNode{
		{ID: 1, ParentID: 0, Name: "Level0", Order: 1},
		{ID: 2, ParentID: 1, Name: "Level1", Order: 1},
		{ID: 3, ParentID: 2, Name: "Level2", Order: 1},
		{ID: 4, ParentID: 3, Name: "Level3", Order: 1},
		{ID: 5, ParentID: 4, Name: "Level4", Order: 1},
	}

	builder := NewTreeBuilder[uint, *TestNode]()
	roots := builder.Build(nodes)

	fmt.Println("\n=== Deep Tree (5 levels) ===")
	levelMap := builder.GetNodesByLevel(roots)
	printLevelNodes(levelMap)

	maxLevel := builder.GetMaxLevel(roots)
	if maxLevel != 4 {
		t.Errorf("Expected max level 4, got %d", maxLevel)
	}

	// 验证每层只有一个节点
	for i := 0; i <= 4; i++ {
		if len(levelMap[i]) != 1 {
			t.Errorf("Level %d: expected 1 node, got %d", i, len(levelMap[i]))
		}
	}
}

// TestEmptyNodes 测试空节点列表
func TestEmptyNodes(t *testing.T) {
	builder := NewTreeBuilder[uint, *TestNode]()
	roots := builder.Build([]*TestNode{})

	if len(roots) != 0 {
		t.Errorf("Expected 0 roots for empty input, got %d", len(roots))
	}

	maxLevel := builder.GetMaxLevel(roots)
	if maxLevel != -1 {
		t.Errorf("Expected max level -1 for empty tree, got %d", maxLevel)
	}
}

// TestBuildWithMap 测试 BuildWithMap 方法
func TestBuildWithMap(t *testing.T) {
	nodes := []*TestNode{
		{ID: 1, ParentID: 0, Name: "Root", Order: 1},
		{ID: 2, ParentID: 1, Name: "Child1", Order: 1},
		{ID: 3, ParentID: 1, Name: "Child2", Order: 2},
	}

	builder := NewTreeBuilder[uint, *TestNode]()
	roots, nodeMap := builder.BuildWithMap(nodes)

	if len(roots) != 1 {
		t.Errorf("Expected 1 root, got %d", len(roots))
	}

	if len(nodeMap) != 3 {
		t.Errorf("Expected 3 nodes in map, got %d", len(nodeMap))
	}

	// 验证可以通过 ID 快速访问节点
	node2, exists := nodeMap[2]
	if !exists || node2.Name != "Child1" {
		t.Error("Node map lookup failed")
	}
}
