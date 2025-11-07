package codegen

import (
	"bytes"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/morehao/golib/gast"
	"github.com/morehao/golib/gutils"
)

const (
	tplFileExtension = ".tpl"
	goFileExtension  = ".go"
)

type ModuleTplAnalysisRes struct {
	PackageName     string
	TableName       string
	StructName      string
	TplAnalysisList []ModuleTplAnalysisItem
}

type TplAnalysisItem struct {
	Template        *template.Template
	TplFilepath     string
	TplFilename     string
	OriginFilename  string
	TargetDir       string
	TargetFilename  string
	TargetFileExist bool
	OriginLayerName LayerName
	LayerName       LayerName
	LayerPrefix     LayerPrefix
}

type ModuleTplAnalysisItem struct {
	TplAnalysisItem
	ModelFields []ModelField
}

type ApiTplAnalysisRes struct {
	PackageName     string
	TplAnalysisList []TplAnalysisItem
}

type GenParams struct {
	ParamsList []GenParamsItem
}

type GenParamsItem struct {
	Template       *template.Template
	TargetDir      string
	TargetFileName string
	ExtraParams    interface{}
}

// 获取指定目录下所有的模板文件
func analysisTplFiles(cfg CommonConfig, defaultTargetFilename string) ([]TplAnalysisItem, error) {
	// 打开指定目录
	file, openErr := os.Open(cfg.TplDir)
	if openErr != nil {
		return nil, openErr
	}
	// 读取目录下所有文件
	tplFilenameList, readErr := file.Readdirnames(-1)
	if readErr != nil {
		return nil, readErr
	}
	var analysisList []TplAnalysisItem
	rootDir := cfg.RootDir
	for _, tplFilename := range tplFilenameList {
		// 判断是否是模板文件
		if gutils.GetFileExtension(tplFilename) != tplFileExtension {
			continue
		}

		// 构造文件层级名称，如controller
		defaultLayerName := LayerName(strings.TrimSuffix(tplFilename, fmt.Sprintf("%s%s", goFileExtension, tplFileExtension)))
		layerName := defaultLayerName
		if specialName, ok := defaultLayerSpecialNameMap[layerName]; ok {
			layerName = specialName
		}
		if customLayerName, ok := cfg.LayerNameMap[defaultLayerName]; ok {
			layerName = customLayerName
		}

		// 构造生成文件所在模块的模块前缀
		defaultLayerPrefix := defaultLayerPrefixMap[defaultLayerName]
		layerPrefix := defaultLayerPrefix
		if prefix, ok := cfg.LayerPrefixMap[defaultLayerName]; ok {
			layerPrefix = prefix
		}

		layerParentDir := rootDir
		// 构造生成文件所在目录的名称
		// 优先使用映射后的 layerName，如果找不到再使用原始的 defaultLayerName
		if customLayerParentDir, ok := cfg.LayerParentDirMap[layerName]; ok {
			layerParentDir = filepath.Join(layerParentDir, customLayerParentDir)
		} else if customLayerParentDir, ok := cfg.LayerParentDirMap[defaultLayerName]; ok {
			layerParentDir = filepath.Join(layerParentDir, customLayerParentDir)
		}
		var targetDir string
		if defaultLayerPrefix.String() == "" {
			targetDir = filepath.Join(layerParentDir, string(layerName))
		} else {
			targetFileParentDir := fmt.Sprintf("%s%s", layerPrefix, strings.ToLower(gutils.SnakeToPascal(cfg.PackageName)))
			targetDir = filepath.Join(layerParentDir, string(layerName), targetFileParentDir)
		}

		// 构造生成文件的文件名称
		originFilename := gutils.TrimFileExtension(gutils.TrimFileExtension(tplFilename))
		var targetFilename string
		switch defaultLayerName {
		case LayerNameRequest, LayerNameResponse:
			targetFilename = fmt.Sprintf("%s%s", originFilename, goFileExtension)
		case LayerNameRouter, LayerNameCode:
			targetFilename = fmt.Sprintf("%s%s", gutils.CamelToSnakeCase(cfg.PackageName), goFileExtension)
		default:
			targetFilename = fmt.Sprintf("%s%s", gutils.TrimFileExtension(defaultTargetFilename), goFileExtension)
		}

		var targetFileExist bool
		if gutils.FileExists(filepath.Join(targetDir, targetFilename)) {
			targetFileExist = true
		}
		tplFilepath := filepath.Join(cfg.TplDir, tplFilename)
		tplInst := template.New(tplFilename).Funcs(cfg.TplFuncMap)
		fileTemplate, parseErr := tplInst.ParseFiles(tplFilepath)
		if parseErr != nil {
			return nil, parseErr
		}

		analysisList = append(analysisList, TplAnalysisItem{
			Template:        fileTemplate,
			TplFilepath:     tplFilepath,
			TplFilename:     tplFilename,
			OriginLayerName: defaultLayerName,
			LayerName:       layerName,
			LayerPrefix:     layerPrefix,
			OriginFilename:  originFilename,
			TargetDir:       targetDir,
			TargetFilename:  targetFilename,
			TargetFileExist: targetFileExist,
		})

	}
	return analysisList, nil
}

func createFile(targetDir, targetFilename string, tpl *template.Template, tplParam interface{}) error {
	if err := gutils.CreateDir(targetDir); err != nil {
		return err
	}
	codeFilepath := filepath.Join(targetDir, targetFilename)

	if gutils.FileExists(codeFilepath) {
		// 文件已存在，写入临时文件，再追加
		tempDir := filepath.Join(targetDir, "tmp")
		tmpFilepath := filepath.Join(tempDir, targetFilename)
		if err := gutils.CreateDir(tempDir); err != nil {
			return err
		}
		defer func() {
			_ = os.RemoveAll(tempDir)
		}()

		// 将模板执行结果写入 buffer，再写入临时文件
		var buf bytes.Buffer
		if err := tpl.Execute(&buf, tplParam); err != nil {
			return err
		}
		if err := os.WriteFile(tmpFilepath, buf.Bytes(), 0666); err != nil {
			return err
		}

		// 判断是否包含 package/import，并决定截断头部
		var appendContent string
		hasPackage, checkPackageErr := gast.HasPackageKeywords(tmpFilepath)
		if checkPackageErr != nil {
			return checkPackageErr
		}
		hasImport, checkImportErr := gast.HasImportKeywords(tmpFilepath)
		if checkImportErr != nil {
			return checkImportErr
		}
		if hasPackage || hasImport {
			content, trimErr := gast.TrimFileTitle(tmpFilepath)
			if trimErr != nil {
				return trimErr
			}
			appendContent = "\n" + content
		} else {
			content, readErr := os.ReadFile(tmpFilepath)
			if readErr != nil {
				return readErr
			}
			appendContent = string(content)
		}

		// 格式化追加内容
		formattedContent, formatErr := format.Source([]byte(appendContent))
		if formatErr != nil {
			return fmt.Errorf("format fail, error: %w", formatErr)
		}

		// 追加写入原文件
		f, openErr := os.OpenFile(codeFilepath, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0666)
		if openErr != nil {
			return openErr
		}
		defer f.Close()

		if _, err := f.Write(formattedContent); err != nil {
			return err
		}
	} else {
		// 文件不存在，生成新文件
		var buf bytes.Buffer
		if err := tpl.Execute(&buf, tplParam); err != nil {
			return err
		}
		formattedContent, formatErr := format.Source(buf.Bytes())
		if formatErr != nil {
			return fmt.Errorf("format fail, error: %w", formatErr)
		}

		f, openErr := os.OpenFile(codeFilepath, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0666)
		if openErr != nil {
			return openErr
		}
		defer f.Close()

		if _, err := f.Write(formattedContent); err != nil {
			return err
		}
	}

	return nil
}
