package gindict

import (
	"github.com/morehao/golib/dict"
)

// toTypeResponse 类型实体转对外契约。
func toTypeResponse(t *dict.DictType) *dictTypeResponse {
	return &dictTypeResponse{
		ID:          t.ID,
		Code:        t.Code,
		Name:        t.Name,
		Status:      string(t.Status),
		Extra:       t.Extra,
		Description: t.Description,
		CreatedAt:   t.CreatedAt.Unix(),
		UpdatedAt:   t.UpdatedAt.Unix(),
	}
}

// toItemResponse 项实体转对外契约。
func toItemResponse(it *dict.DictItem) *dictItemResponse {
	return &dictItemResponse{
		ID:          it.ID,
		TypeCode:    it.TypeCode,
		Value:       it.Value,
		Label:       it.Label,
		ParentID:    it.ParentID,
		Level:       it.Level,
		Sort:        it.Sort,
		Status:      string(it.Status),
		Extra:       it.Extra,
		Description: it.Description,
		CreatedAt:   it.CreatedAt.Unix(),
		UpdatedAt:   it.UpdatedAt.Unix(),
	}
}

// toItemResponses 批量转换。
func toItemResponses(items []*dict.DictItem) []*dictItemResponse {
	out := make([]*dictItemResponse, 0, len(items))
	for _, it := range items {
		out = append(out, toItemResponse(it))
	}
	return out
}

// toTreeNode 递归转换树节点。
func toTreeNode(n *dict.TreeNode) *treeNodeResponse {
	node := &treeNodeResponse{
		dictItemResponse: toItemResponse(n.DictItem),
		Orphan:           n.Orphan,
	}
	node.Children = make([]*treeNodeResponse, 0, len(n.Children))
	for _, child := range n.Children {
		node.Children = append(node.Children, toTreeNode(child))
	}
	return node
}

// toTreeNodes 批量转换树。
func toTreeNodes(nodes []*dict.TreeNode) []*treeNodeResponse {
	out := make([]*treeNodeResponse, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, toTreeNode(n))
	}
	return out
}

// toIntegrityResponse 巡检结果转对外契约。
func toIntegrityResponse(r *dict.IntegrityReport) *integrityResponse {
	resp := &integrityResponse{
		TypeCode: r.TypeCode,
		Checked:  r.Checked,
		Healthy:  r.Healthy(),
		Orphans:  r.Orphans,
		Issues:   make([]issueResponse, 0, len(r.Issues)),
	}
	if resp.Orphans == nil {
		resp.Orphans = []string{}
	}
	for _, is := range r.Issues {
		resp.Issues = append(resp.Issues, issueResponse{
			ItemID: is.ItemID,
			Value:  is.Value,
			Kind:   is.Kind,
			Detail: is.Detail,
		})
	}
	return resp
}
