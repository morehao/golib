package codegen

import (
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func openMySQLForTest(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := "root:123456@tcp(127.0.0.1:3306)/demo?charset=utf8mb4&parseTime=True"
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Skipf("skip mysql-dependent test: %v", err)
	}
	return db
}

func openPostgresForTest(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := "host=127.0.0.1 user=postgres password=123456 dbname=demo port=5432 sslmode=disable TimeZone=Asia/Shanghai"
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Skipf("skip postgres-dependent test: %v", err)
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
		PackageName   string
		StructName    string
		DBServiceName string
	}
	var params []GenParamsItem
	for _, tplItem := range templateParam.TplAnalysisList {
		params = append(params, GenParamsItem{
			TargetDir:      tplItem.TargetDir,
			TargetFileName: tplItem.TargetFilename,
			Template:       tplItem.Template,
			ExtraParams: &Param{
				PackageName:   templateParam.PackageName,
				StructName:    templateParam.StructName,
				DBServiceName: "mysql",
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
				IsPrimaryKey: field.ColumnKey == "PRI",
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
	assert.Nil(t, getParamErr)
	type Param struct {
		PackageName   string
		StructName    string
		DBServiceName string
	}
	var params []GenParamsItem
	for _, tplItem := range templateParam.TplAnalysisList {
		params = append(params, GenParamsItem{
			TargetDir:      tplItem.TargetDir,
			TargetFileName: tplItem.TargetFilename,
			Template:       tplItem.Template,
			ExtraParams: &Param{
				PackageName:   templateParam.PackageName,
				StructName:    templateParam.StructName,
				DBServiceName: "postgresql",
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
				IsPrimaryKey: field.ColumnKey == "PRI",
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
