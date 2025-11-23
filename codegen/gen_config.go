package codegen

import (
	"strings"
	"text/template"

	"github.com/morehao/golib/gutil"
)

type CommonConfig struct {
	PackageName       string                    // 包名
	TplDir            string                    // 模板目录
	RootDir           string                    // 生成文件的根目录
	LayerParentDirMap map[LayerName]string      // 各层级父目录，如果为空则使用默认规则
	LayerNameMap      map[LayerName]LayerName   // 各层级名称，如果为空则使用默认规则
	LayerPrefixMap    map[LayerName]LayerPrefix // 各层级前缀，如果为空则使用默认规则
	TplFuncMap        template.FuncMap          // 模板函数
}

type ModuleCfg struct {
	CommonConfig
	TableName     string            `validate:"required"` // 表名
	ColumnTypeMap map[string]string // 表字段类型映射，入股为空则使用默认规则
}

type ApiCfg struct {
	CommonConfig
	TargetFilename string // 目标文件名
}

func (cfg *CommonConfig) format() {
	cfg.PackageName = strings.ToLower(gutil.SnakeToPascal(cfg.PackageName))
}

type LayerName string

type LayerPrefix string

func (lp LayerPrefix) String() string {
	return string(lp)
}

const (
	LayerNameRouter     LayerName = "router"
	LayerNameController LayerName = "controller"
	LayerNameService    LayerName = "service"
	LayerNameDto        LayerName = "dto"
	LayerNameRequest    LayerName = "request"
	LayerNameResponse   LayerName = "response"
	LayerNameModel      LayerName = "model"
	LayerNameDao        LayerName = "dao"
	LayerNameCode       LayerName = "code"
	LayerNameObject     LayerName = "object"

	defaultLayerNameRequest  LayerName = "dto"
	defaultLayerNameResponse LayerName = "dto"

	defaultLayerPrefixController LayerPrefix = "ctr"
	defaultLayerPrefixService    LayerPrefix = "svc"
	defaultLayerPrefixDto        LayerPrefix = "dto"
	defaultLayerPrefixDao        LayerPrefix = "dao"
	defaultLayerPrefixObject     LayerPrefix = "obj"
)

var defaultLayerPrefixMap = map[LayerName]LayerPrefix{
	LayerNameController: defaultLayerPrefixController,
	LayerNameService:    defaultLayerPrefixService,
	LayerNameDao:        defaultLayerPrefixDao,
	LayerNameDto:        defaultLayerPrefixDto,
	LayerNameRequest:    defaultLayerPrefixDto,
	LayerNameResponse:   defaultLayerPrefixDto,
	LayerNameObject:     defaultLayerPrefixObject,
}

var defaultLayerSpecialNameMap = map[LayerName]LayerName{
	LayerNameRequest:  defaultLayerNameRequest,
	LayerNameResponse: defaultLayerNameResponse,
}
