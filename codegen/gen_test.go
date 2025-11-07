package codegen

import (
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func TestGenModuleCode(t *testing.T) {
	dsn := "root:123456@tcp(127.0.0.1:3306)/demo?charset=utf8mb4&parseTime=True"
	db, openErr := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	assert.Nil(t, openErr)
	// 获取当前的运行路径
	workDir, getErr := os.Getwd()
	assert.Nil(t, getErr)
	tplDir := fmt.Sprintf("%s/tplExample/module", workDir)
	rootDir := fmt.Sprintf("%s/tmp", workDir)
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
	tplDir := fmt.Sprintf("%s/tplExample/api", workDir)
	rootDir := fmt.Sprintf("%s/tmp", workDir)
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
	dsn := "root:123456@tcp(127.0.0.1:3306)/demo?charset=utf8mb4&parseTime=True"
	db, openErr := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	assert.Nil(t, openErr)
	// 获取当前的运行路径
	workDir, getErr := os.Getwd()
	assert.Nil(t, getErr)
	tplDir := fmt.Sprintf("%s/tplExample/model", workDir)
	rootDir := fmt.Sprintf("%s/tmp", workDir)
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
