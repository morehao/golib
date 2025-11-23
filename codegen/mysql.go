package codegen

import (
	"fmt"

	"github.com/morehao/golib/gutil"
	"gorm.io/gorm"
)

type mysqlImpl struct {
}

func (impl *mysqlImpl) GetModuleTemplateParam(db *gorm.DB, cfg *ModuleCfg) (*ModuleTplAnalysisRes, error) {
	dbName, getDbNameErr := getDbName(db)
	if getDbNameErr != nil {
		return nil, getDbNameErr
	}
	tableList, getTableErr := getTableList(db, dbName)
	if getTableErr != nil {
		return nil, getTableErr
	}
	tableMap := tableList.ToMap()
	if _, ok := tableMap[cfg.TableName]; !ok {
		return nil, fmt.Errorf("table %s not exist", cfg.TableName)
	}

	modelFieldList, getFieldErr := impl.getModelField(db, dbName, cfg)
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

func (impl *mysqlImpl) getModelField(db *gorm.DB, dbName string, cfg *ModuleCfg) ([]ModelField, error) {
	var entities []mysqlTableColumn
	getColumnSql := fmt.Sprintf("SELECT * FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_SCHEMA = '%s' AND TABLE_NAME = '%s';", dbName, cfg.TableName)
	if err := db.Raw(getColumnSql).Scan(&entities).Error; err != nil {
		return nil, err
	}
	columnTypeMap := mysqlDefaultColumnTypeMap
	if len(cfg.ColumnTypeMap) > 0 {
		columnTypeMap = cfg.ColumnTypeMap
	}
	var modelFieldList []ModelField
	for _, v := range entities {
		item := ModelField{
			FieldName:    gutil.SnakeToPascal(v.ColumnName),
			FieldType:    columnTypeMap[v.DataType],
			ColumnName:   v.ColumnName,
			ColumnType:   v.ColumnType,
			ColumnKey:    v.ColumnKey,
			IsNullable:   v.IsNullable == "YES",
			DefaultValue: v.ColumnDefault.String,
			Comment:      v.ColumnComment,
		}
		modelFieldList = append(modelFieldList, item)
	}
	return modelFieldList, nil
}

var mysqlDefaultColumnTypeMap = map[string]string{
	"tinyint":    "int8",
	"smallint":   "int16",
	"mediumint":  "int32",
	"int":        "int32",
	"integer":    "int32",
	"bigint":     "uint",
	"float":      "float64",
	"double":     "float64",
	"decimal":    "string", // 或者使用 "big.Rat" 或 "float64"，取决于精度需求
	"date":       "time.Time",
	"datetime":   "time.Time",
	"timestamp":  "time.Time",
	"time":       "time.Duration",
	"year":       "int16",
	"char":       "string",
	"varchar":    "string",
	"text":       "string",
	"tinytext":   "string",
	"mediumtext": "string",
	"longtext":   "string",
	"blob":       "[]byte",
	"tinyblob":   "[]byte",
	"mediumblob": "[]byte",
	"longblob":   "[]byte",
	"enum":       "string", // 或者自定义类型
	"set":        "string", // 或者自定义类型，可能是字符串切片
	"bit":        "[]byte", // 或者 "uint64"，取决于位数
	"binary":     "[]byte",
	"varbinary":  "[]byte",
	"json":       "json.RawMessage", // 或者 "map[string]interface{}" 或自定义结构体
	"bool":       "bool",
	"boolean":    "bool",
}
