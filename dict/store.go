package dict

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/morehao/golib/dbaccess/gormdao"
	"gorm.io/gorm"
)

// Source 字典数据的读取抽象。本期唯一实现是直读 DB 的 dbSource；
// 阶段三的缓存装饰器在此接口上叠加（写入路径永不走缓存，因此不在本接口内）。
//
// 约定：读方法返回的 typeStatusResult 表示"所属类型是否存在、是否停用"，
// 由 joinItems 的第一次类型查询取得（见下）；项查询紧随其后，两次均按 app 收敛。
type Source interface {
	// GetType 按 code 点查类型；不存在返回 (nil, nil)。
	GetType(ctx context.Context, code string) (*DictType, error)
	// GetTypes 批量点查类型（codes 为空时返回空切片且不查库）。
	GetTypes(ctx context.Context, codes []string) ([]*DictType, error)
	// ListTypes 管理端条件分页查询（分组/状态/编码均为可选筛选）。
	ListTypes(ctx context.Context, cond *TypeCond) ([]*DictType, int64, error)

	// GetItems 取某类型下的项（按 sort, id 升序）。
	// includeDisabled=false 时只返回 enabled 的项，但类型停用与否由 typeStatusResult 表达。
	GetItems(ctx context.Context, typeCode string, includeDisabled bool) ([]*DictItem, typeStatusResult, error)
	// GetChildren 取某父节点下的直接子项；parentID 为空串表示根节点集合。
	GetChildren(ctx context.Context, typeCode, parentID string, includeDisabled bool) ([]*DictItem, typeStatusResult, error)
	// GetItemByValue 单值点查；item 为 nil 表示"该 value 不在字典内"。
	GetItemByValue(ctx context.Context, typeCode, value string) (*DictItem, typeStatusResult, error)
	// GetItemsByValues 批量点查，只返回命中的项。
	GetItemsByValues(ctx context.Context, typeCode string, values []string) ([]*DictItem, typeStatusResult, error)

	// GetItemByID 按主键点查项；不存在返回 (nil, nil)。
	GetItemByID(ctx context.Context, id string) (*DictItem, error)
	// GetSubtree 取某节点自身及其全部后代（含已停用项），按 level, sort, id 升序。
	GetSubtree(ctx context.Context, typeCode, pathPrefix string) ([]*DictItem, error)
	// CountItems 统计类型下的项数（含已停用项）。
	CountItems(ctx context.Context, typeCode string) (int64, error)
}

// typeStatusResult 类型存在性与状态，由 joinItems 的第一次类型查询取得。
type typeStatusResult struct {
	status Status
	found  bool
}

func (t typeStatusResult) disabled() bool {
	return t.found && t.status == StatusDisabled
}

// ---- 直读 DB 的实现 ----

type dbSource struct {
	dbGetter gormdao.DBGetter
}

func newDBSource(dbGetter gormdao.DBGetter) *dbSource {
	return &dbSource{dbGetter: dbGetter}
}

// joinItems 取某类型下的项，并同时返回所属类型状态。
//
// 实现刻意用**两次独立查询**而不是项表 JOIN 类型表：
//   - 先按 code 取类型（类型表自身的 app 过滤由 gormdao 回调注入到单表 WHERE）；
//   - 若类型存在，再按 type_code 取项（项表自身的 app 过滤同样由回调注入）。
//
// 为什么不再 JOIN：gormdao 的租户回调只对**单张受控表**追加 app 条件，不解析 JOIN
// 里的第二张表（见 gormdao.RegisterTenantScope 注释）。把类型状态塞进 JOIN 的第二张
// 表时，那张表的 app 条件无法被回调收敛，存在跨 app 串数据的隐患。改成两次单表查询后，
// 每次查询都由回调在同表 WHERE 上收敛 app，从根消除"别名/JOIN 不收敛"问题。
//
// 代价是常规读多一次往返（字典读是高频路径）。但类型点查结果稳定、可被上层缓存装饰器
// 直接命中（GetType 走缓存），且正确性优先于一次往返——原设计文档中的"JOIN 省一次
// 往返"取舍在多 app 隔离下不再成立。
func (s *dbSource) joinItems(ctx context.Context, typeCode string, extraWhere string, extraArgs []any, includeDisabled bool) ([]*DictItem, typeStatusResult, error) {
	// 第一次：取类型（app 由回调收敛到类型表 WHERE）；类型不存在即返回空集，
	// 调用方据 typeStatusResult.found 区分"类型不存在"与"类型下无项/无命中"。
	entity, err := s.GetType(ctx, typeCode)
	if err != nil {
		return nil, typeStatusResult{}, err
	}
	if entity == nil {
		return nil, typeStatusResult{}, nil
	}
	ts := typeStatusResult{status: entity.Status, found: true}

	// 第二次：按 type_code 取项（app 由回调收敛到项表 WHERE）。
	db := s.dbGetter(ctx).Table(tableNameItem).Where("type_code = ?", typeCode)
	if !includeDisabled {
		db = db.Where("status = ?", StatusEnabled)
	}
	if extraWhere != "" {
		db = db.Where(extraWhere, extraArgs...)
	}

	var items []*DictItem
	if err := db.Order("sort ASC, id ASC").Find(&items).Error; err != nil {
		return nil, ts, err
	}
	return items, ts, nil
}

func (s *dbSource) GetType(ctx context.Context, code string) (*DictType, error) {
	var entity DictType
	err := s.dbGetter(ctx).Table(tableNameType).
		Where("code = ?", code).
		Take(&entity).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &entity, nil
}

func (s *dbSource) GetTypes(ctx context.Context, codes []string) ([]*DictType, error) {
	if len(codes) == 0 {
		return nil, nil
	}
	var list []*DictType
	err := s.dbGetter(ctx).Table(tableNameType).
		Where("code IN ?", codes).
		Find(&list).Error
	if err != nil {
		return nil, err
	}
	return list, nil
}

func (s *dbSource) ListTypes(ctx context.Context, cond *TypeCond) ([]*DictType, int64, error) {
	if cond == nil {
		cond = &TypeCond{}
	}
	if cond.PageSize > gormdao.MaxPageSize {
		cond.PageSize = gormdao.MaxPageSize
	}
	db := s.dbGetter(ctx).Table(tableNameType)
	cond.BuildCondition(db, tableNameType)

	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if cond.Page > 0 && cond.PageSize > 0 {
		db = db.Offset((cond.Page - 1) * cond.PageSize).Limit(cond.PageSize)
	}
	var list []*DictType
	if err := db.Find(&list).Error; err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

func (s *dbSource) GetItems(ctx context.Context, typeCode string, includeDisabled bool) ([]*DictItem, typeStatusResult, error) {
	items, ts, err := s.joinItems(ctx, typeCode, "", nil, includeDisabled)
	if err != nil {
		return nil, ts, err
	}
	if items == nil {
		items = []*DictItem{}
	}
	// 类型存在但暂无项：返回空切片而非 nil，避免调用方/JSON 出现 null
	return items, ts, nil
}

func (s *dbSource) GetChildren(ctx context.Context, typeCode, parentID string, includeDisabled bool) ([]*DictItem, typeStatusResult, error) {
	items, ts, err := s.joinItems(ctx, typeCode, "parent_id = ?", []any{parentID}, includeDisabled)
	if err != nil {
		return nil, ts, err
	}
	if items == nil {
		items = []*DictItem{}
	}
	return items, ts, nil
}

func (s *dbSource) GetItemByValue(ctx context.Context, typeCode, value string) (*DictItem, typeStatusResult, error) {
	items, ts, err := s.joinItems(ctx, typeCode, "value = ?", []any{value}, true)
	if err != nil {
		return nil, ts, err
	}
	if len(items) > 0 {
		return items[0], ts, nil
	}
	return nil, ts, nil
}

func (s *dbSource) GetItemsByValues(ctx context.Context, typeCode string, values []string) ([]*DictItem, typeStatusResult, error) {
	if len(values) == 0 {
		return nil, typeStatusResult{}, nil
	}
	items, ts, err := s.joinItems(ctx, typeCode, "value IN ?", []any{values}, true)
	if err != nil {
		return nil, ts, err
	}
	return items, ts, nil
}

func (s *dbSource) GetItemByID(ctx context.Context, id string) (*DictItem, error) {
	var entity DictItem
	err := s.dbGetter(ctx).Table(tableNameItem).Where("id = ?", id).Take(&entity).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &entity, nil
}

func (s *dbSource) GetSubtree(ctx context.Context, typeCode, pathPrefix string) ([]*DictItem, error) {
	var list []*DictItem
	err := s.dbGetter(ctx).Table(tableNameItem).
		Where("type_code = ? AND path LIKE ?", typeCode, escapeLike(pathPrefix)+"%").
		Order("level ASC, sort ASC, id ASC").
		Find(&list).Error
	if err != nil {
		return nil, err
	}
	return list, nil
}

func (s *dbSource) CountItems(ctx context.Context, typeCode string) (int64, error) {
	var count int64
	err := s.dbGetter(ctx).Table(tableNameItem).
		Where("type_code = ?", typeCode).
		Count(&count).Error
	return count, err
}

// ---- store：写路径 + 树不变量（唯一维护者） ----

type store struct {
	dbGetter gormdao.DBGetter
	source   Source
	maxLevel int
	// 泛型实参用值类型 DictType/DictItem（Entity 约束只要求 TableName() string），
	// 这样 dao 的 GetByID/GetByCond 直接返回 *DictType，Insert 直接收 *DictType。
	typeDao *gormdao.Dao[DictType, []DictType, string]
	itemDao *gormdao.Dao[DictItem, []DictItem, string]
}

func newStore(dbGetter gormdao.DBGetter, maxLevel int, source Source) *store {
	if source == nil {
		source = newDBSource(dbGetter)
	}
	return &store{
		dbGetter: dbGetter,
		source:   source,
		maxLevel: maxLevel,
		typeDao:  gormdao.NewDao[DictType, []DictType, string](tableNameType, "dict", dbGetter, gormdao.WithoutSoftDelete()),
		itemDao:  gormdao.NewDao[DictItem, []DictItem, string](tableNameItem, "dict", dbGetter, gormdao.WithoutSoftDelete()),
	}
}

// escapeLike 转义 LIKE 的通配符（id 链只含十六进制与连字符，正常不会命中，
// 但路径前缀来自用户传入的 id，转义可避免 `%`/`_` 造成误匹配）。
func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

// ---- 写路径：类型 ----

func (s *store) createType(ctx context.Context, entity *DictType) error {
	return s.dbGetter(ctx).Transaction(func(tx *gorm.DB) error {
		typeDao := s.typeDao.WithTx(tx)
		exists, err := s.typeExists(ctx, tx, entity.Code)
		if err != nil {
			return err
		}
		if exists {
			return ErrCodeDuplicated
		}
		if err := typeDao.Insert(ctx, entity); err != nil {
			// 并发下唯一约束兜底：不解析方言错误串，改以存在性复核判定
			if ok, checkErr := s.typeExists(ctx, tx, entity.Code); checkErr == nil && ok {
				return ErrCodeDuplicated
			}
			return err
		}
		return nil
	})
}

func (s *store) updateType(ctx context.Context, id string, updateMap map[string]any) error {
	entity, err := s.typeDao.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if entity == nil {
		return ErrTypeNotFound
	}
	if len(updateMap) == 0 {
		return nil
	}
	return s.typeDao.UpdateMap(ctx, id, updateMap)
}

func (s *store) deleteType(ctx context.Context, id string, cascade bool) error {
	entity, err := s.typeDao.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if entity == nil {
		return ErrTypeNotFound
	}

	count, err := s.source.CountItems(ctx, entity.Code)
	if err != nil {
		return err
	}
	if count > 0 && !cascade {
		return ErrTypeHasItems
	}

	return s.dbGetter(ctx).Transaction(func(tx *gorm.DB) error {
		if cascade {
			if err := tx.Table(tableNameItem).
				Where("type_code = ?", entity.Code).
				Delete(&DictItem{}).Error; err != nil {
				return err
			}
		}
		return s.typeDao.WithTx(tx).Delete(ctx, id, "")
	})
}

func (s *store) createItem(ctx context.Context, item *DictItem) error {
	return s.dbGetter(ctx).Transaction(func(tx *gorm.DB) error {
		typeDao := s.typeDao.WithTx(tx)
		itemDao := s.itemDao.WithTx(tx)

		entity, err := typeDao.GetByCond(ctx, &TypeCond{Code: item.TypeCode})
		if err != nil {
			return err
		}
		if entity == nil {
			return ErrTypeNotFound
		}

		// 主键先于 path 生成：path 由 id 链构成
		if item.ID == "" {
			item.ID = newID()
		}

		if item.ParentID == "" {
			item.Level = 1
			item.Path = "/" + item.ID + "/"
		} else {
			parent, err := itemDao.GetByID(ctx, item.ParentID)
			if err != nil {
				return err
			}
			if parent == nil {
				return ErrItemNotFound
			}
			if parent.TypeCode != item.TypeCode {
				return ErrParentTypeMismatch
			}
			newLevel := parent.Level + 1
			if newLevel > s.maxLevel {
				return ErrLevelExceeded
			}
			item.Level = newLevel
			item.Path = parent.Path + item.ID + "/"
		}

		exists, err := s.itemExists(ctx, tx, item.TypeCode, item.Value)
		if err != nil {
			return err
		}
		if exists {
			return ErrValueDuplicated
		}

		if err := itemDao.Insert(ctx, item); err != nil {
			if ok, checkErr := s.itemExists(ctx, tx, item.TypeCode, item.Value); checkErr == nil && ok {
				return ErrValueDuplicated
			}
			return err
		}
		return nil
	})
}

func (s *store) updateItem(ctx context.Context, id string, updateMap map[string]any) error {
	entity, err := s.itemDao.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if entity == nil {
		return ErrItemNotFound
	}
	if len(updateMap) == 0 {
		return nil
	}
	return s.itemDao.UpdateMap(ctx, id, updateMap)
}

func (s *store) deleteItem(ctx context.Context, id string, cascade bool) error {
	item, err := s.source.GetItemByID(ctx, id)
	if err != nil {
		return err
	}
	if item == nil {
		// 幂等：已删除视为成功
		return nil
	}

	descendants, err := s.countSubtree(ctx, item.TypeCode, item.Path, true)
	if err != nil {
		return err
	}
	if descendants > 0 && !cascade {
		return ErrItemHasChildren
	}

	return s.dbGetter(ctx).Transaction(func(tx *gorm.DB) error {
		if cascade {
			if err := tx.Table(tableNameItem).
				Where("type_code = ? AND path LIKE ?", item.TypeCode, escapeLike(item.Path)+"%").
				Delete(&DictItem{}).Error; err != nil {
				return err
			}
			return nil
		}
		return s.itemDao.WithTx(tx).Delete(ctx, id, "")
	})
}

// moveItem 移动节点：先校验（任一失败数据不变），再在单事务内重写自身与整棵子树。
//
// 子树 path 重写用一条 REPLACE(path, 旧前缀, 新前缀) + 常量 level 增量完成：
// 移动使全子树层级发生同一个位移，故 level 可一条表达式更新，无需逐行 CASE。
// 旧前缀是"根到该节点的 id 链"，id 在 path 中唯一出现，不会误替换其他片段。
func (s *store) moveItem(ctx context.Context, itemID, newParentID string) error {
	item, err := s.source.GetItemByID(ctx, itemID)
	if err != nil {
		return err
	}
	if item == nil {
		return ErrItemNotFound
	}
	if newParentID == itemID {
		return ErrParentCycle
	}

	var newParent *DictItem
	if newParentID != "" {
		newParent, err = s.source.GetItemByID(ctx, newParentID)
		if err != nil {
			return err
		}
		if newParent == nil {
			return ErrItemNotFound
		}
		if newParent.TypeCode != item.TypeCode {
			return ErrParentTypeMismatch
		}
		// newParent 落在 item 的子树内 → 成环
		if strings.HasPrefix(newParent.Path, item.Path) {
			return ErrParentCycle
		}
	}

	newLevel := 1
	newPath := "/" + item.ID + "/"
	if newParent != nil {
		newLevel = newParent.Level + 1
		newPath = newParent.Path + item.ID + "/"
	}

	maxLevelInSubtree, err := s.maxLevelInSubtree(ctx, item.TypeCode, item.Path)
	if err != nil {
		return err
	}
	subtreeHeight := maxLevelInSubtree - item.Level + 1
	if newLevel+subtreeHeight-1 > s.maxLevel {
		return ErrLevelExceeded
	}

	if newPath == item.Path && newParentID == item.ParentID {
		return nil
	}

	return s.dbGetter(ctx).Transaction(func(tx *gorm.DB) error {
		if err := s.itemDao.WithTx(tx).UpdateMap(ctx, item.ID, map[string]any{"parent_id": newParentID}); err != nil {
			return err
		}
		return tx.Table(tableNameItem).
			Where("type_code = ? AND path LIKE ?", item.TypeCode, escapeLike(item.Path)+"%").
			Updates(map[string]any{
				"path":       gorm.Expr("REPLACE(path, ?, ?)", item.Path, newPath),
				"level":      gorm.Expr("level + ?", newLevel-item.Level),
				"updated_at": time.Now(),
			}).Error
	})
}

// ---- store 私有写路径查询（不走 Source：这些查询永不可缓存） ----

func (s *store) typeExists(ctx context.Context, tx *gorm.DB, code string) (bool, error) {
	var count int64
	err := tx.Table(tableNameType).Where("code = ?", code).Count(&count).Error
	return count > 0, err
}

func (s *store) itemExists(ctx context.Context, tx *gorm.DB, typeCode, value string) (bool, error) {
	var count int64
	err := tx.Table(tableNameItem).
		Where("type_code = ? AND value = ?", typeCode, value).
		Count(&count).Error
	return count > 0, err
}

// countSubtree 统计 pathPrefix 子树的行数；excludeSelf=true 时不含该前缀对应的节点自身。
func (s *store) countSubtree(ctx context.Context, typeCode, pathPrefix string, excludeSelf bool) (int64, error) {
	db := s.dbGetter(ctx).Table(tableNameItem).
		Where("type_code = ? AND path LIKE ?", typeCode, escapeLike(pathPrefix)+"%")
	if excludeSelf {
		db = db.Where("path <> ?", pathPrefix)
	}
	var count int64
	err := db.Count(&count).Error
	return count, err
}

func (s *store) maxLevelInSubtree(ctx context.Context, typeCode, pathPrefix string) (int, error) {
	var maxLevel *int
	err := s.dbGetter(ctx).Table(tableNameItem).
		Select("MAX(level)").
		Where("type_code = ? AND path LIKE ?", typeCode, escapeLike(pathPrefix)+"%").
		Scan(&maxLevel).Error
	if err != nil {
		return 0, err
	}
	if maxLevel == nil {
		return 0, ErrItemNotFound
	}
	return *maxLevel, nil
}

// danglingRow 项表里 type_code 在类型表中找不到对应行的记录。
// 只 SELECT 必然非空的列，避免把可空列扫进 string。
type danglingRow struct {
	ID    string `gorm:"column:id"`
	Value string `gorm:"column:value"`
}

// findDanglingItems 找出 type_code 下所有"归属类型不存在"的项：
// 类型被直删后残留的行对读接口不可见，属于沉默的数据损坏。
//
// 实现用两次单表查询而非 LEFT JOIN：
//   - 先按 code 查类型（app 由 gormdao 回调收敛到类型表 WHERE）；
//   - 类型存在 → 无残留（同 app 同 code 的项都归属该类型），直接返回空；
//   - 类型不存在 → 该 code 下所有项都是残留（再按 type_code 查项，app 同样由回调收敛）。
//
// 这样每次查询都只命中一张受控表，避免了 LEFT JOIN 中第二张类型表的 app 条件
// 无法被回调收敛的问题（与 joinItems 的改造同理）。
func (s *store) findDanglingItems(ctx context.Context, typeCode string) ([]danglingRow, error) {
	entity, err := s.source.GetType(ctx, typeCode)
	if err != nil {
		return nil, err
	}
	if entity != nil {
		return nil, nil
	}
	var rows []danglingRow
	err = s.dbGetter(ctx).Table(tableNameItem).
		Select("id AS id, value AS value").
		Where("type_code = ?", typeCode).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// checkIntegrity 巡检：只报告不自动修复（修复动作应由人工决策后走 MoveItem 等显式入口）。
func (s *store) checkIntegrity(ctx context.Context, typeCode string) (*IntegrityReport, error) {
	// 先查"归属类型不存在"的残留项：类型行被绕过 DeleteType 直删时，
	// 这些行对读接口完全不可见，是最危险的沉默损坏，必须在 ErrTypeNotFound 之前先报出来。
	dangling, err := s.findDanglingItems(ctx, typeCode)
	if err != nil {
		return nil, err
	}

	entity, err := s.source.GetType(ctx, typeCode)
	if err != nil {
		return nil, err
	}
	if entity == nil {
		if len(dangling) == 0 {
			return nil, ErrTypeNotFound
		}
		report := &IntegrityReport{TypeCode: typeCode, Checked: len(dangling)}
		for _, row := range dangling {
			report.Issues = append(report.Issues, ItemIssue{
				ItemID: row.ID, Value: row.Value, Kind: IssueTypeMismatch,
				Detail: "类型 " + typeCode + " 在类型表中不存在，该行对读接口不可见（手工删类型行会留下残留项）",
			})
		}
		return report, nil
	}

	items, _, err := s.source.GetItems(ctx, typeCode, true)
	if err != nil {
		return nil, err
	}

	report := &IntegrityReport{TypeCode: typeCode, Checked: len(items)}
	byID := make(map[string]*DictItem, len(items))
	for _, item := range items {
		byID[item.ID] = item
	}

	for _, item := range items {
		if item.Level > s.maxLevel {
			report.Issues = append(report.Issues, ItemIssue{ItemID: item.ID, Value: item.Value, Kind: IssueLevelOverflow, Detail: "level 超过 MaxLevel"})
		}

		if item.ParentID == "" {
			if want := "/" + item.ID + "/"; item.Path != want {
				report.Issues = append(report.Issues, ItemIssue{ItemID: item.ID, Value: item.Value, Kind: IssuePathMismatch, Detail: "根节点 path 应为 /自身/，实际 " + item.Path})
			}
			if item.Level != 1 {
				report.Issues = append(report.Issues, ItemIssue{ItemID: item.ID, Value: item.Value, Kind: IssueLevelMismatch, Detail: "根节点 level 应为 1"})
			}
		} else {
			if parent, ok := byID[item.ParentID]; !ok {
				report.Orphans = append(report.Orphans, item.ID)
				report.Issues = append(report.Issues, ItemIssue{ItemID: item.ID, Value: item.Value, Kind: IssueOrphan, Detail: "parent_id 指向不存在的项 " + item.ParentID})
			} else {
				if want := parent.Path + item.ID + "/"; item.Path != want {
					report.Issues = append(report.Issues, ItemIssue{ItemID: item.ID, Value: item.Value, Kind: IssuePathMismatch, Detail: "path 与父节点不一致"})
				}
				if item.Level != parent.Level+1 {
					report.Issues = append(report.Issues, ItemIssue{ItemID: item.ID, Value: item.Value, Kind: IssueLevelMismatch, Detail: "level 应为父节点 +1"})
				}
			}
		}

		if hasCycle(item, byID) {
			report.Issues = append(report.Issues, ItemIssue{ItemID: item.ID, Value: item.Value, Kind: IssueCycle, Detail: "parent_id 链成环"})
		}
	}

	// 刻意不检查"同层 sort 相同"：sort 只决定展示顺序，重复是合法的（未设置时默认全 0），
	// 把它当成问题会产生大量误报，反而让巡检失去意义。
	return report, nil
}

// hasCycle 沿 parent_id 链上溯，判断是否回到自身（最多走 len(byID) 步）。
func hasCycle(item *DictItem, byID map[string]*DictItem) bool {
	visited := make(map[string]bool, 8)
	current := item
	for i := 0; i <= len(byID); i++ {
		if current.ParentID == "" {
			return false
		}
		if visited[current.ID] {
			return true
		}
		visited[current.ID] = true
		parent, ok := byID[current.ParentID]
		if !ok {
			return false
		}
		current = parent
	}
	return true
}
