package codegen

import (
	"fmt"
	"os"
	"testing"

	"github.com/morehao/golib/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func init() {
	testutil.Load()
}

// pingOrSkip 真正验证数据库可达后返回连接。
//
// gorm.Open 是**惰性**的：它只解析 DSN，不建立连接，数据库不可达时照样返回
// (db, nil)。原先的 skip 守卫直接依赖 gorm.Open 的 error，因此从未生效 ——
// 测试带着一个连不上的 db 继续往下跑，最终在首次查询处以 "table user not exist"
// 失败，再被 assert.Nil 的非致命性放大成 nil 解引用 panic。
func pingOrSkip(t *testing.T, db *gorm.DB, kind string, err error) *gorm.DB {
	t.Helper()
	if err != nil {
		t.Skipf("skip %s-dependent test: %v", kind, err)
	}
	sqlDB, sqlErr := db.DB()
	if sqlErr != nil {
		t.Skipf("skip %s-dependent test: %v", kind, sqlErr)
	}
	if pingErr := sqlDB.Ping(); pingErr != nil {
		t.Skipf("skip %s-dependent test: %v", kind, pingErr)
	}
	return db
}

func openMySQLForTest(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := testutil.GetEnv(testutil.MySQLDSN, "root:123456@tcp(127.0.0.1:3306)/demo?charset=utf8mb4&parseTime=True")
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	return pingOrSkip(t, db, "mysql", err)
}

func openPostgresForTest(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := testutil.GetEnv(testutil.PostgresDSN, "host=127.0.0.1 user=postgres password=123456 dbname=demo port=5432 sslmode=disable TimeZone=Asia/Shanghai")
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	db = pingOrSkip(t, db, "postgres", err)
	// 仓库里没有任何迁移/SQL 文件为 demo 库建表（已核实：无 *.sql、无 AutoMigrate），
	// 这两个用例只是要拿一张真实表的元数据做解析。缺表时按"依赖未就绪"跳过并
	// 说明缺什么，而不是报成语义不明的 "table user not exist" 失败 —— 后者看起来
	// 像代码缺陷，实际是环境未预置。
	if !db.Migrator().HasTable("user") {
		t.Skip("skip postgres-dependent test: demo 库缺少 user 表（需手工建表，仓库未提供迁移）")
	}
	return db
}

func TestGenModuleCode(t *testing.T) {
	db := openMySQLForTest(t)
	// 获取当前的运行路径
	workDir, getErr := os.Getwd()
	assert.Nil(t, getErr)
	tplDir := fmt.Sprintf("%s/example/tplExample/module", workDir)
	rootDir := t.TempDir()
	layerParentDirMap := map[LayerName]string{
		LayerNameModel:      "model",
		LayerNameDao:        "dao",
		LayerNameController: "internal",
		LayerNameDto:        "internal",
		LayerNameService:    "internal",
	}
	// layerNameMap := map[LayerName]LayerName{
	// 	LayerNameCode:  "code",
	// 	LayerNameModel: "mysqlmodel",
	// 	LayerNameDao:   "mysqldao",
	// }
	LayerPrefixMap := map[LayerName]LayerPrefix{
		LayerNameService: "srv",
	}
	cfg := &ModuleCfg{
		CommonConfig: CommonConfig{
			PackageName:       "user",
			TplDir:            tplDir,
			RootDir:           rootDir,
			LayerParentDirMap: layerParentDirMap,
			// LayerNameMap:      layerNameMap,
			LayerPrefixMap: LayerPrefixMap,
		},
		TableName: "user",
	}
	autoCodeTool := NewGenerator()
	templateParam, getParamErr := autoCodeTool.AnalysisModuleTpl(db, cfg)
	assert.Nil(t, getParamErr)
	type Param struct {
		PackageName          string
		StructName           string
		DBServiceName        string
		ControllerImportPath string
	}
	var params []GenParamsItem
	for _, tplItem := range templateParam.TplAnalysisList {
		params = append(params, GenParamsItem{
			TargetDir:      tplItem.TargetDir,
			TargetFileName: tplItem.TargetFilename,
			Template:       tplItem.Template,
			ExtraParams: &Param{
				PackageName:          templateParam.PackageName,
				StructName:           templateParam.StructName,
				DBServiceName:        "mysql",
				ControllerImportPath: "github.com/morehao/golib/codegen/example/internal/controller/ctruser",
			},
		})
	}
	err := autoCodeTool.Gen(&GenParams{
		ParamsList: params,
	})
	assert.Nil(t, err)
}

func TestGenApiCode(t *testing.T) {
	// 获取当前的运行路径
	workDir, getErr := os.Getwd()
	assert.Nil(t, getErr)
	tplDir := fmt.Sprintf("%s/example/tplExample/api", workDir)
	rootDir := t.TempDir()
	cfg := &ApiCfg{
		CommonConfig: CommonConfig{
			PackageName: "user",
			TplDir:      tplDir,
			RootDir:     rootDir,
		},
		TargetFilename: "user.go",
	}
	autoCodeTool := NewGenerator()
	templateParam, getParamErr := autoCodeTool.AnalysisApiTpl(cfg)
	assert.Nil(t, getParamErr)
	type Param struct {
		PackageName  string
		FunctionName string
	}
	var params []GenParamsItem
	for _, tplItem := range templateParam.TplAnalysisList {
		params = append(params, GenParamsItem{
			TargetDir:      tplItem.TargetDir,
			TargetFileName: tplItem.TargetFilename,
			Template:       tplItem.Template,
			ExtraParams: &Param{
				PackageName:  templateParam.PackageName,
				FunctionName: "UserSetting",
			},
		})
	}
	err := autoCodeTool.Gen(&GenParams{
		ParamsList: params,
	})
	assert.Nil(t, err)
}

func TestGenModelCode(t *testing.T) {
	db := openMySQLForTest(t)
	// 获取当前的运行路径
	workDir, getErr := os.Getwd()
	assert.Nil(t, getErr)
	tplDir := fmt.Sprintf("%s/example/tplExample/model", workDir)
	rootDir := t.TempDir()
	layerParentDirMap := map[LayerName]string{
		LayerNameModel: "model",
		LayerNameDao:   "dao",
	}
	layerNameMap := map[LayerName]LayerName{
		LayerNameCode:  "code",
		LayerNameModel: "mysqlmodel",
		LayerNameDao:   "mysqldao",
	}
	LayerPrefixMap := map[LayerName]LayerPrefix{
		LayerNameService: "srv",
	}
	cfg := &ModuleCfg{
		CommonConfig: CommonConfig{
			PackageName:       "user",
			TplDir:            tplDir,
			RootDir:           rootDir,
			LayerParentDirMap: layerParentDirMap,
			LayerNameMap:      layerNameMap,
			LayerPrefixMap:    LayerPrefixMap,
		},
		TableName: "user",
	}
	autoCodeTool := NewGenerator()
	templateParam, getParamErr := autoCodeTool.AnalysisModuleTpl(db, cfg)
	assert.Nil(t, getParamErr)
	type ModelFieldItem struct {
		FieldName    string
		ColumnName   string
		Comment      string
		IsPrimaryKey bool
	}
	type Param struct {
		PackageName      string
		DBServiceName    string
		StructName       string
		TableName        string
		TableDescription string
		ModelFields      []ModelFieldItem
	}

	var params []GenParamsItem
	for _, tplItem := range templateParam.TplAnalysisList {
		var modelFields []ModelFieldItem

		for _, field := range tplItem.ModelFields {
			modelFields = append(modelFields, ModelFieldItem{
				FieldName:    field.FieldName,
				ColumnName:   field.ColumnName,
				Comment:      field.Comment,
				IsPrimaryKey: field.IsPrimaryKey,
			})
		}

		param := GenParamsItem{
			TargetDir:      tplItem.TargetDir,
			TargetFileName: tplItem.TargetFilename,
			Template:       tplItem.Template,
			ExtraParams: &Param{
				PackageName:   templateParam.PackageName,
				StructName:    templateParam.StructName,
				ModelFields:   modelFields,
				DBServiceName: "mysql",
			},
		}
		params = append(params, param)
	}
	err := autoCodeTool.Gen(&GenParams{
		ParamsList: params,
	})
	assert.Nil(t, err)
}

func TestGenModuleCodeWithPostgreSQL(t *testing.T) {
	db := openPostgresForTest(t)
	// 获取当前的运行路径
	workDir, getErr := os.Getwd()
	assert.Nil(t, getErr)
	tplDir := fmt.Sprintf("%s/example/tplExample/module", workDir)
	rootDir := t.TempDir()
	layerParentDirMap := map[LayerName]string{
		LayerNameModel:      "model",
		LayerNameDao:        "dao",
		LayerNameController: "internal",
		LayerNameDto:        "internal",
		LayerNameService:    "internal",
	}
	LayerPrefixMap := map[LayerName]LayerPrefix{
		LayerNameService: "srv",
	}
	cfg := &ModuleCfg{
		CommonConfig: CommonConfig{
			PackageName:       "user",
			TplDir:            tplDir,
			RootDir:           rootDir,
			LayerParentDirMap: layerParentDirMap,
			LayerPrefixMap:    LayerPrefixMap,
		},
		TableName: "user",
	}
	autoCodeTool := NewGenerator()
	templateParam, getParamErr := autoCodeTool.AnalysisModuleTpl(db, cfg)
	require.Nil(t, getParamErr)
	type Param struct {
		PackageName          string
		StructName           string
		DBServiceName        string
		ControllerImportPath string
	}
	var params []GenParamsItem
	for _, tplItem := range templateParam.TplAnalysisList {
		params = append(params, GenParamsItem{
			TargetDir:      tplItem.TargetDir,
			TargetFileName: tplItem.TargetFilename,
			Template:       tplItem.Template,
			ExtraParams: &Param{
				PackageName:          templateParam.PackageName,
				StructName:           templateParam.StructName,
				DBServiceName:        "postgresql",
				ControllerImportPath: "github.com/morehao/golib/codegen/example/postgresql/internal/controller/ctruser",
			},
		})
	}
	err := autoCodeTool.Gen(&GenParams{
		ParamsList: params,
	})
	assert.Nil(t, err)
}

func TestGenModelCodeWithPostgreSQL(t *testing.T) {
	db := openPostgresForTest(t)
	// 获取当前的运行路径
	workDir, getErr := os.Getwd()
	assert.Nil(t, getErr)
	tplDir := fmt.Sprintf("%s/example/tplExample/model", workDir)
	rootDir := t.TempDir()
	layerParentDirMap := map[LayerName]string{
		LayerNameModel: "model",
		LayerNameDao:   "dao",
	}
	layerNameMap := map[LayerName]LayerName{
		LayerNameCode:  "code",
		LayerNameModel: "pgmodel",
		LayerNameDao:   "pgdao",
	}
	LayerPrefixMap := map[LayerName]LayerPrefix{
		LayerNameService: "srv",
	}
	cfg := &ModuleCfg{
		CommonConfig: CommonConfig{
			PackageName:       "user",
			TplDir:            tplDir,
			RootDir:           rootDir,
			LayerParentDirMap: layerParentDirMap,
			LayerNameMap:      layerNameMap,
			LayerPrefixMap:    LayerPrefixMap,
		},
		TableName: "user",
	}
	autoCodeTool := NewGenerator()
	templateParam, getParamErr := autoCodeTool.AnalysisModuleTpl(db, cfg)
	require.Nil(t, getParamErr)
	type ModelFieldItem struct {
		FieldName    string
		ColumnName   string
		Comment      string
		IsPrimaryKey bool
	}
	type Param struct {
		PackageName      string
		DBServiceName    string
		StructName       string
		TableName        string
		TableDescription string
		ModelFields      []ModelFieldItem
	}

	var params []GenParamsItem
	for _, tplItem := range templateParam.TplAnalysisList {
		var modelFields []ModelFieldItem

		for _, field := range tplItem.ModelFields {
			modelFields = append(modelFields, ModelFieldItem{
				FieldName:    field.FieldName,
				ColumnName:   field.ColumnName,
				Comment:      field.Comment,
				IsPrimaryKey: field.IsPrimaryKey,
			})
		}

		param := GenParamsItem{
			TargetDir:      tplItem.TargetDir,
			TargetFileName: tplItem.TargetFilename,
			Template:       tplItem.Template,
			ExtraParams: &Param{
				PackageName:   templateParam.PackageName,
				StructName:    templateParam.StructName,
				ModelFields:   modelFields,
				DBServiceName: "postgresql",
			},
		}
		params = append(params, param)
	}
	err := autoCodeTool.Gen(&GenParams{
		ParamsList: params,
	})
	assert.Nil(t, err)
}
