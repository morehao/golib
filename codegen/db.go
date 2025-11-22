package codegen

import (
	"database/sql"
	"fmt"

	"gorm.io/gorm"
)

const (
	dbTypeMysql     = "mysql"
	dbTypePostgresql = "postgres"

	ColumnKeyPRI = "PRI" // 主键
)

// mysqlTableColumn represents a column in the INFORMATION_SCHEMA.COLUMNS table
type mysqlTableColumn struct {
	ColumnName             string         `gorm:"column:COLUMN_NAME"`              // 列名
	DataType               string         `gorm:"column:DATA_TYPE"`                // 列的数据类型，如int
	ColumnType             string         `gorm:"column:COLUMN_TYPE"`              // 列的完整类型定义，如varchar(255)
	IsNullable             string         `gorm:"column:IS_NULLABLE"`              // 列是否允许 NULL 值。可能的值为 YES 或 NO
	ColumnDefault          sql.NullString `gorm:"column:COLUMN_DEFAULT"`           // 列的默认值
	ColumnComment          string         `gorm:"column:COLUMN_COMMENT"`           // 列的注释
	CharacterMaximumLength sql.NullInt64  `gorm:"column:CHARACTER_MAXIMUM_LENGTH"` // 字符串列的最大长度
	NumericPrecision       sql.NullInt64  `gorm:"column:NUMERIC_PRECISION"`        // 数值列的精度
	NumericScale           sql.NullInt64  `gorm:"column:NUMERIC_SCALE"`            // 数值列的小数位数
	DatetimePrecision      sql.NullInt64  `gorm:"column:DATETIME_PRECISION"`       // 日期时间列的精度
	CharacterSetName       sql.NullString `gorm:"column:CHARACTER_SET_NAME"`       // 字符串列的字符集名称
	CollationName          sql.NullString `gorm:"column:COLLATION_NAME"`           // 字符串列的排序规则名称
	OrdinalPosition        int64          `gorm:"column:ORDINAL_POSITION"`         // 列在表中的位置，从 1 开始
	ColumnKey              string         `gorm:"column:COLUMN_KEY"`               // 表示列是否是索引的一部分,可能的值为 PRI（主键）, UNI（唯一索引）, MUL（非唯一索引）
	Extra                  string         `gorm:"column:EXTRA"`                    // 列的额外信息，如 auto_increment
	Privileges             string         `gorm:"column:PRIVILEGES"`               // 与列相关的权限，如 select,insert,update,references
	GenerationExpression   sql.NullString `gorm:"column:GENERATION_EXPRESSION"`    // 生成列的表达式
}

// postgresqlTableColumn represents a column in the INFORMATION_SCHEMA.COLUMNS table for PostgreSQL
type postgresqlTableColumn struct {
	ColumnName             string         `gorm:"column:column_name"`              // 列名
	DataType               string         `gorm:"column:data_type"`                // 列的数据类型，如integer
	UdtName                string         `gorm:"column:udt_name"`                 // PostgreSQL 用户定义类型名，通常与 data_type 相同
	IsNullable             string         `gorm:"column:is_nullable"`              // 列是否允许 NULL 值。可能的值为 YES 或 NO
	ColumnDefault          sql.NullString `gorm:"column:column_default"`          // 列的默认值
	CharacterMaximumLength sql.NullInt64  `gorm:"column:character_maximum_length"` // 字符串列的最大长度
	NumericPrecision       sql.NullInt64  `gorm:"column:numeric_precision"`        // 数值列的精度
	NumericScale           sql.NullInt64  `gorm:"column:numeric_scale"`            // 数值列的小数位数
	DatetimePrecision      sql.NullInt64  `gorm:"column:datetime_precision"`      // 日期时间列的精度
	OrdinalPosition        int64          `gorm:"column:ordinal_position"`         // 列在表中的位置，从 1 开始
	TableSchema            string         `gorm:"column:table_schema"`            // 表所在的 schema
	TableName              string         `gorm:"column:table_name"`              // 表名
	ColumnComment          string         `gorm:"column:column_comment"`          // 列的注释（通过 JOIN pg_description 获取）
}

type ModelField struct {
	FieldName    string // 字段名称
	FieldType    string // 字段数据类型，如int、string
	ColumnName   string // 列名
	ColumnType   string // 列数据类型，如varchar(255)
	ColumnKey    string // 索引类型，如PRI（主键）, UNI（唯一索引）, MUL（非唯一索引）
	IsNullable   bool   // 是否允许为空
	DefaultValue string // 默认值
	Comment      string // 字段注释
}

type TableList []string

func (l TableList) ToMap() map[string]struct{} {
	m := make(map[string]struct{}, len(l))
	for _, v := range l {
		m[v] = struct{}{}
	}
	return m
}

func getTableList(db *gorm.DB, dbName string) (tableList TableList, err error) {
	getTableSql := fmt.Sprintf("SELECT TABLE_NAME FROM INFORMATION_SCHEMA.TABLES WHERE TABLE_SCHEMA = '%s';", dbName)
	if err = db.Raw(getTableSql).Scan(&tableList).Error; err != nil {
		return nil, err
	}
	return tableList, nil
}

func getDbName(db *gorm.DB) (dbName string, err error) {
	var entity struct {
		DbName string `gorm:"column:db_name"`
	}
	if err = db.Raw("SELECT DATABASE() db_name").Scan(&entity).Error; err != nil {
		return "", err
	}
	return entity.DbName, nil
}

func getPostgresqlDbName(db *gorm.DB) (dbName string, err error) {
	var entity struct {
		DbName string `gorm:"column:current_database"`
	}
	if err = db.Raw("SELECT current_database() AS current_database").Scan(&entity).Error; err != nil {
		return "", err
	}
	return entity.DbName, nil
}

func getPostgresqlTableList(db *gorm.DB, schemaName string) (tableList TableList, err error) {
	if schemaName == "" {
		schemaName = "public"
	}
	getTableSql := fmt.Sprintf("SELECT table_name FROM information_schema.tables WHERE table_schema = '%s' AND table_type = 'BASE TABLE';", schemaName)
	if err = db.Raw(getTableSql).Scan(&tableList).Error; err != nil {
		return nil, err
	}
	return tableList, nil
}
