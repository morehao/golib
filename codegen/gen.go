package codegen

import (
	"fmt"
	"strings"

	"gorm.io/gorm"
)

type Generator interface {
	AnalysisModuleTpl(db *gorm.DB, cfg *ModuleCfg) (*ModuleTplAnalysisRes, error)
	AnalysisApiTpl(cfg *ApiCfg) (*ApiTplAnalysisRes, error)
	Gen(params *GenParams) error
}

func NewGenerator() Generator {
	return &generatorImpl{}
}

type generatorImpl struct{}

func (impl *generatorImpl) AnalysisModuleTpl(db *gorm.DB, cfg *ModuleCfg) (*ModuleTplAnalysisRes, error) {
	if db == nil {
		return nil, fmt.Errorf("db is nil")
	}
	if err := impl.checkModuleCfg(cfg); err != nil {
		return nil, err
	}
	cfg.format()
	dbType := db.Dialector.Name()
	switch dbType {
	case dbTypeMysql:
		inst := &mysqlImpl{}
		return inst.GetModuleTemplateParam(db, cfg)
	case dbTypePostgresql:
		inst := &postgresqlImpl{}
		return inst.GetModuleTemplateParam(db, cfg)
	default:
		return nil, fmt.Errorf("unsupported database type: %s", dbType)
	}
}

func (impl *generatorImpl) checkModuleCfg(cfg *ModuleCfg) error {
	if cfg == nil {
		return fmt.Errorf("cfg is nil")
	}
	requiredFields := map[string]string{
		"packageName": cfg.PackageName,
		"tableName":   cfg.TableName,
		"tplDir":      cfg.TplDir,
		"rootDir":     cfg.RootDir,
	}

	for field, value := range requiredFields {
		if value == "" {
			return fmt.Errorf("%s is required", field)
		}
	}
	return nil
}

func (impl *generatorImpl) AnalysisApiTpl(cfg *ApiCfg) (*ApiTplAnalysisRes, error) {
	if err := impl.checkControllerCfg(cfg); err != nil {
		return nil, err
	}
	// 解析模板文件
	tplAnalysisList, analysisErr := analysisTplFiles(cfg.CommonConfig, cfg.TargetFilename)
	if analysisErr != nil {
		return nil, analysisErr
	}
	// 构造模板参数
	cfg.format()
	res := &ApiTplAnalysisRes{
		PackageName:     cfg.PackageName,
		TplAnalysisList: tplAnalysisList,
	}
	return res, nil
}

func (impl *generatorImpl) checkControllerCfg(cfg *ApiCfg) error {
	if cfg == nil {
		return fmt.Errorf("cfg is nil")
	}
	requiredFields := map[string]string{
		"packageName":    cfg.PackageName,
		"targetFilename": cfg.TargetFilename,
		"tplDir":         cfg.TplDir,
		"rootDir":        cfg.RootDir,
	}

	for field, value := range requiredFields {
		if value == "" {
			return fmt.Errorf("%s is required", field)
		}
	}
	if !strings.HasSuffix(cfg.TargetFilename, goFileExtension) {
		return fmt.Errorf("targetFilename should end with %s", goFileExtension)
	}
	return nil
}

func (impl *generatorImpl) Gen(params *GenParams) error {
	if err := impl.checkGenParams(params); err != nil {
		return err
	}
	for _, v := range params.ParamsList {
		if err := createFile(v.TargetDir, v.TargetFileName, v.Template, v.ExtraParams); err != nil {
			return err
		}
	}
	return nil
}

func (impl *generatorImpl) checkGenParams(params *GenParams) error {
	if params == nil {
		return fmt.Errorf("params is nil")
	}
	if len(params.ParamsList) == 0 {
		return fmt.Errorf("params is required")
	}
	for _, v := range params.ParamsList {
		if v.TargetDir == "" {
			return fmt.Errorf("target dir is required")
		}
		if v.TargetFileName == "" {
			return fmt.Errorf("target file name is required")
		}
		if v.Template == nil {
			return fmt.Errorf("template is required")
		}
		if v.ExtraParams == nil {
			return fmt.Errorf("params is required")
		}
	}
	return nil
}
