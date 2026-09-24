package dict

import (
	"context"
	"sort"
	"strings"
)

// GetType 取类型元信息；类型不存在返回 ErrTypeNotFound。
func (d *Dict) GetType(ctx context.Context, code string) (*DictType, error) {
	if code == "" {
		return nil, ErrCodeRequired
	}
	entity, err := d.store.source.GetType(ctx, code)
	if err != nil {
		return nil, err
	}
	if entity == nil {
		return nil, ErrTypeNotFound
	}
	return entity, nil
}

// GetTypes 批量取类型元信息，只返回存在的类型（缺失的 code 不出现在结果里）。
// 一次查询取回全部，用于页面一次渲染多个字典的类型名。
func (d *Dict) GetTypes(ctx context.Context, codes ...string) (map[string]*DictType, error) {
	result := make(map[string]*DictType, len(codes))
	if len(codes) == 0 {
		return result, nil
	}
	if len(codes) > MaxBatchSize {
		return nil, ErrBatchTooLarge
	}
	list, err := d.store.source.GetTypes(ctx, dedupStrings(codes))
	if err != nil {
		return nil, err
	}
	for _, entity := range list {
		result[entity.Code] = entity
	}
	return result, nil
}

// GetItems 取类型下的全部项（默认只 enabled，按 sort, id 升序）。
//
// 查询次数：类型有项时恒为 1 次（项表 JOIN 类型表，顺带判定类型状态）；
// 仅当结果为空时补一次类型点查，用于区分 ErrTypeNotFound 与"类型下暂无项"。
func (d *Dict) GetItems(ctx context.Context, typeCode string, opts ...QueryOption) ([]*DictItem, error) {
	if typeCode == "" {
		return nil, ErrCodeRequired
	}
	q := buildQueryOptions(opts)
	items, ts, err := d.store.source.GetItems(ctx, typeCode, q.includeDisabled)
	if err != nil {
		return nil, err
	}
	if err := checkTypeUsable(ts, q.includeDisabled); err != nil {
		return nil, err
	}
	return items, nil
}

// GetChildren 取某父节点下的直接子项，用于大字典逐层懒加载（省/市/区）。
// parentID 传空串表示取根节点集合。
func (d *Dict) GetChildren(ctx context.Context, typeCode, parentID string, opts ...QueryOption) ([]*DictItem, error) {
	if typeCode == "" {
		return nil, ErrCodeRequired
	}
	q := buildQueryOptions(opts)
	items, ts, err := d.store.source.GetChildren(ctx, typeCode, parentID, q.includeDisabled)
	if err != nil {
		return nil, err
	}
	if err := checkTypeUsable(ts, q.includeDisabled); err != nil {
		return nil, err
	}
	return items, nil
}

// GetItem 按 (类型, 码值) 点查单项。
// 四态错误可判别：ErrTypeNotFound / ErrTypeDisabled / ErrItemNotFound / ErrItemDisabled。
func (d *Dict) GetItem(ctx context.Context, typeCode, value string, opts ...QueryOption) (*DictItem, error) {
	if typeCode == "" {
		return nil, ErrCodeRequired
	}
	if value == "" {
		return nil, ErrValueRequired
	}
	q := buildQueryOptions(opts)
	item, ts, err := d.store.source.GetItemByValue(ctx, typeCode, value)
	if err != nil {
		return nil, err
	}
	if err := checkTypeUsable(ts, q.includeDisabled); err != nil {
		return nil, err
	}
	if item == nil {
		return nil, ErrItemNotFound
	}
	if item.Status != StatusEnabled && !q.includeDisabled {
		return nil, ErrItemDisabled
	}
	return item, nil
}

// Exists 判断码值是否可用（存在且启用）。fail-closed：任何 false 都伴随 sentinel——
// ErrItemNotFound 表示"值不在字典内"，ErrItemDisabled 表示"值已停用"，
// ErrTypeNotFound / ErrTypeDisabled 表示字典本身有问题。调用方据此把
// "用户传了非法值"（映射 400）与"字典配置缺失/停用"（告警）分开处理。
//
// 批量校验请改用 BatchExists：那里对"单个值缺失"返回 false 而不报错。
func (d *Dict) Exists(ctx context.Context, typeCode, value string) (bool, error) {
	_, err := d.GetItem(ctx, typeCode, value)
	if err != nil {
		return false, err
	}
	return true, nil
}

// BatchExists 批量校验码值，一次查询完成。
//
// 返回 map 覆盖**全部入参值**（含重复值与缺失值）：
//   - 命中且启用 → true
//   - 未命中或已停用 → false（不报错，便于一次性收集全部非法值）
//
// 类型级问题（不存在/停用）仍然返回 error，因为那属于配置故障而非用户输入问题。
func (d *Dict) BatchExists(ctx context.Context, typeCode string, values []string) (map[string]bool, error) {
	if typeCode == "" {
		return nil, ErrCodeRequired
	}
	result := make(map[string]bool, len(values))
	if len(values) == 0 {
		return result, nil
	}
	if len(values) > MaxBatchSize {
		return nil, ErrBatchTooLarge
	}

	uniq := dedupStrings(values)
	items, ts, err := d.store.source.GetItemsByValues(ctx, typeCode, uniq)
	if err != nil {
		return nil, err
	}
	if err := checkTypeUsable(ts, false); err != nil {
		return nil, err
	}

	found := make(map[string]bool, len(items))
	for _, item := range items {
		found[item.Value] = item.Status == StatusEnabled
	}
	for _, value := range uniq {
		result[value] = found[value]
	}
	return result, nil
}

// BuildTree 取整类型的项并在内存组装为树（1 次查询）。
// parent_id 指向集合外节点的项会被提升为根并标记 Orphan=true，不丢弃、不报错。
func (d *Dict) BuildTree(ctx context.Context, typeCode string, opts ...QueryOption) ([]*TreeNode, error) {
	items, err := d.GetItems(ctx, typeCode, opts...)
	if err != nil {
		return nil, err
	}
	return assembleTree(items), nil
}

// Subtree 取以 value 对应节点为根的子树（含自身）。
// 查询次数为 2（先按 value 定位节点，再按物化 path 前缀取子树）。
func (d *Dict) Subtree(ctx context.Context, typeCode, value string, opts ...QueryOption) ([]*DictItem, error) {
	if value == "" {
		return nil, ErrValueRequired
	}
	q := buildQueryOptions(opts)
	root, err := d.GetItem(ctx, typeCode, value, opts...)
	if err != nil {
		return nil, err
	}
	items, err := d.store.source.GetSubtree(ctx, typeCode, root.Path)
	if err != nil {
		return nil, err
	}
	if q.includeDisabled {
		return items, nil
	}
	enabled := make([]*DictItem, 0, len(items))
	for _, item := range items {
		if item.Status == StatusEnabled {
			enabled = append(enabled, item)
		}
	}
	return enabled, nil
}

// SubtreeValues 取子树的全部码值（含根），保持 level/sort 顺序，用于级联筛选。
func (d *Dict) SubtreeValues(ctx context.Context, typeCode, value string, opts ...QueryOption) ([]string, error) {
	items, err := d.Subtree(ctx, typeCode, value, opts...)
	if err != nil {
		return nil, err
	}
	values := make([]string, 0, len(items))
	for _, item := range items {
		values = append(values, item.Value)
	}
	return values, nil
}

// ---- 内部助手 ----

func checkTypeUsable(ts typeStatusResult, includeDisabled bool) error {
	if !ts.found {
		return ErrTypeNotFound
	}
	if ts.disabled() && !includeDisabled {
		return ErrTypeDisabled
	}
	return nil
}

// assembleTree 由扁平列表组装树：一次遍历建索引，再一次挂接。
func assembleTree(items []*DictItem) []*TreeNode {
	nodes := make(map[string]*TreeNode, len(items))
	for _, item := range items {
		nodes[item.ID] = &TreeNode{DictItem: item}
	}

	roots := make([]*TreeNode, 0, len(items))
	for _, item := range items {
		node := nodes[item.ID]
		parent, ok := nodes[item.ParentID]
		if item.ParentID == "" || !ok {
			if item.ParentID != "" {
				node.Orphan = true
			}
			roots = append(roots, node)
			continue
		}
		parent.Children = append(parent.Children, node)
	}

	sortTree(roots)
	return roots
}

func sortTree(nodes []*TreeNode) {
	sort.SliceStable(nodes, func(i, j int) bool {
		if nodes[i].Sort != nodes[j].Sort {
			return nodes[i].Sort < nodes[j].Sort
		}
		return nodes[i].ID < nodes[j].ID
	})
	for _, node := range nodes {
		if len(node.Children) > 0 {
			sortTree(node.Children)
		}
	}
}

// dedupStrings 按首次出现顺序去重。
func dedupStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

// isBlank 判断字符串是否只含空白。
func isBlank(s string) bool {
	return strings.TrimSpace(s) == ""
}
