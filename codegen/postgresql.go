package codegen

import (
	"fmt"

	"github.com/morehao/golib/gutil"
	"gorm.io/gorm"
)

type postgresqlImpl struct {
}

func (impl *postgresqlImpl) GetModuleTemplateParam(db *gorm.DB, cfg *ModuleCfg) (*ModuleTplAnalysisRes, error) {
	// PostgreSQL 默认使用 public schema
	tableList, getTableErr := getPostgresqlTableList(db, "public")
	if getTableErr != nil {
		return nil, getTableErr
	}
	tableMap := tableList.ToMap()
	if _, ok := tableMap[cfg.TableName]; !ok {
		return nil, fmt.Errorf("table %s not exist", cfg.TableName)
	}

	modelFieldList, getFieldErr := impl.getModelField(db, "public", cfg)
	if getFieldErr != nil {
		return nil, getFieldErr
	}

	// 获取模板文件
	tplAnalysisList, analysisErr := analysisTplFiles(cfg.CommonConfig, cfg.TableName)
	if analysisErr != nil {
		return nil, analysisErr
	}

	// 构造模板参数
	var moduleAnalysisList []ModuleTplAnalysisItem
	for _, v := range tplAnalysisList {
		moduleAnalysisList = append(moduleAnalysisList, ModuleTplAnalysisItem{
			TplAnalysisItem: v,
			ModelFields:     modelFieldList,
		})
	}
	structName := gutil.SnakeToPascal(cfg.TableName)
	res := &ModuleTplAnalysisRes{
		PackageName:     cfg.PackageName,
		TableName:       cfg.TableName,
		StructName:      structName,
		TplAnalysisList: moduleAnalysisList,
	}
	return res, nil
}

func (impl *postgresqlImpl) getModelField(db *gorm.DB, schemaName string, cfg *ModuleCfg) ([]ModelField, error) {
	// 查询列信息，同时获取注释
	// PostgreSQL 的注释存储在 pg_description 系统表中
	getColumnSql := fmt.Sprintf(`
		SELECT 
			c.column_name,
			c.data_type,
			c.udt_name,
			c.is_nullable,
			c.column_default,
			c.character_maximum_length,
			c.numeric_precision,
			c.numeric_scale,
			c.datetime_precision,
			c.ordinal_position,
			c.table_schema,
			c.table_name,
			COALESCE(pd.description, '') AS column_comment
		FROM information_schema.columns c
		LEFT JOIN pg_class pc ON pc.relname = c.table_name
		LEFT JOIN pg_namespace pn ON pn.oid = pc.relnamespace AND pn.nspname = c.table_schema
		LEFT JOIN pg_description pd ON pd.objoid = pc.oid AND pd.objsubid = c.ordinal_position
		WHERE c.table_schema = '%s' AND c.table_name = '%s'
		ORDER BY c.ordinal_position;
	`, schemaName, cfg.TableName)

	var entities []postgresqlTableColumn
	if err := db.Raw(getColumnSql).Scan(&entities).Error; err != nil {
		return nil, err
	}

	// 查询主键信息
	primaryKeys, pkErr := impl.getPrimaryKeys(db, schemaName, cfg.TableName)
	if pkErr != nil {
		return nil, pkErr
	}

	columnTypeMap := postgresqlDefaultColumnTypeMap
	if len(cfg.ColumnTypeMap) > 0 {
		columnTypeMap = cfg.ColumnTypeMap
	}

	var modelFieldList []ModelField
	for _, v := range entities {
		// 判断是否是主键
		columnKey := ""
		if _, isPK := primaryKeys[v.ColumnName]; isPK {
			columnKey = ColumnKeyPRI
		}

		// 构建完整的列类型（包含长度等信息）
		columnType := impl.buildColumnType(v)

		item := ModelField{
			FieldName:    gutil.SnakeToPascal(v.ColumnName),
			FieldType:    columnTypeMap[v.UdtName],
			ColumnName:   v.ColumnName,
			ColumnType:   columnType,
			ColumnKey:    columnKey,
			IsNullable:   v.IsNullable == "YES",
			DefaultValue: v.ColumnDefault.String,
			Comment:      v.ColumnComment,
		}
		// 如果类型映射中没有找到，使用 data_type 作为后备
		if item.FieldType == "" {
			item.FieldType = columnTypeMap[v.DataType]
		}
		// 如果还是没有找到，使用默认的 string
		if item.FieldType == "" {
			item.FieldType = "string"
		}
		modelFieldList = append(modelFieldList, item)
	}
	return modelFieldList, nil
}

// getPrimaryKeys 获取表的主键列名
func (impl *postgresqlImpl) getPrimaryKeys(db *gorm.DB, schemaName, tableName string) (map[string]struct{}, error) {
	getPkSql := fmt.Sprintf(`
		SELECT kcu.column_name
		FROM information_schema.table_constraints tc
		JOIN information_schema.key_column_usage kcu
			ON tc.constraint_name = kcu.constraint_name
			AND tc.table_schema = kcu.table_schema
		WHERE tc.constraint_type = 'PRIMARY KEY'
			AND tc.table_schema = '%s'
			AND tc.table_name = '%s';
	`, schemaName, tableName)

	var pkColumns []string
	if err := db.Raw(getPkSql).Scan(&pkColumns).Error; err != nil {
		return nil, err
	}

	pkMap := make(map[string]struct{})
	for _, col := range pkColumns {
		pkMap[col] = struct{}{}
	}
	return pkMap, nil
}

// buildColumnType 构建完整的列类型字符串
func (impl *postgresqlImpl) buildColumnType(col postgresqlTableColumn) string {
	columnType := col.UdtName
	if col.CharacterMaximumLength.Valid {
		columnType = fmt.Sprintf("%s(%d)", col.UdtName, col.CharacterMaximumLength.Int64)
	} else if col.NumericPrecision.Valid {
		if col.NumericScale.Valid && col.NumericScale.Int64 > 0 {
			columnType = fmt.Sprintf("%s(%d,%d)", col.UdtName, col.NumericPrecision.Int64, col.NumericScale.Int64)
		} else {
			columnType = fmt.Sprintf("%s(%d)", col.UdtName, col.NumericPrecision.Int64)
		}
	}
	return columnType
}

var postgresqlDefaultColumnTypeMap = map[string]string{
	// 整数类型
	"int2":        "int16", // smallint
	"int4":        "int32", // integer
	"int8":        "int64", // bigint
	"smallint":    "int16",
	"integer":     "int32",
	"bigint":      "int64",
	"serial":      "int32", // serial
	"bigserial":   "int64", // bigserial
	"smallserial": "int16", // smallserial

	// 浮点类型
	"real":             "float32", // real
	"float4":           "float32",
	"double precision": "float64", // double precision
	"float8":           "float64",
	"numeric":          "string", // numeric/decimal，使用 string 保持精度
	"decimal":          "string",

	// 布尔类型
	"bool":    "bool",
	"boolean": "bool",

	// 字符类型
	"char":    "string",
	"varchar": "string",
	"text":    "string",
	"bpchar":  "string", // char 的内部名称
	"name":    "string", // PostgreSQL 系统类型

	// 日期时间类型
	"date":        "time.Time",
	"time":        "time.Time",
	"timetz":      "time.Time", // time with time zone
	"timestamp":   "time.Time",
	"timestamptz": "time.Time", // timestamp with time zone
	"interval":    "time.Duration",

	// JSON 类型
	"json":  "json.RawMessage",
	"jsonb": "json.RawMessage",

	// 二进制类型
	"bytea": "[]byte",

	// UUID 类型
	"uuid": "string", // 或者使用 "github.com/google/uuid".UUID

	// 数组类型（基础类型）
	"_int2":    "[]int16",   // smallint[]
	"_int4":    "[]int32",   // integer[]
	"_int8":    "[]int64",   // bigint[]
	"_text":    "[]string",  // text[]
	"_varchar": "[]string",  // varchar[]
	"_bool":    "[]bool",    // boolean[]
	"_float4":  "[]float32", // real[]
	"_float8":  "[]float64", // double precision[]

	// 网络地址类型
	"inet":    "string", // IP 地址
	"cidr":    "string", // 网络地址
	"macaddr": "string", // MAC 地址

	// 几何类型
	"point":   "string", // 点
	"line":    "string", // 线
	"lseg":    "string", // 线段
	"box":     "string", // 矩形
	"path":    "string", // 路径
	"polygon": "string", // 多边形
	"circle":  "string", // 圆

	// 其他类型
	"money":    "string", // 货币类型
	"xml":      "string", // XML 类型
	"tsvector": "string", // 全文搜索向量
	"tsquery":  "string", // 全文搜索查询
}
