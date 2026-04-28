package gvalidate

import (
	"errors"
	"fmt"
	"reflect"
	"sync"

	"github.com/go-playground/locales/zh_Hans_CN"
	unTrans "github.com/go-playground/universal-translator"
	"github.com/go-playground/validator/v10"
	zhTrans "github.com/go-playground/validator/v10/translations/zh"
)

// ── 错误类型 ────────────────────────────────────────────────────────

type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

func (e *FieldError) Error() string { return e.Message }

type Errors []*FieldError

func (e Errors) Error() string {
	if len(e) == 0 {
		return ""
	}
	return e[0].Message
}

func (e Errors) First() *FieldError {
	if len(e) == 0 {
		return nil
	}
	return e[0]
}

func (e Errors) ToMap() map[string]string {
	m := make(map[string]string, len(e))
	for _, fe := range e {
		m[fe.Field] = fe.Message
	}
	return m
}

// AsErrors 从 error 中提取 Errors，方便调用方做类型断言
func AsErrors(err error) (Errors, bool) {
	var e Errors
	if errors.As(err, &e) {
		return e, true
	}
	return nil, false
}

// ── 内部单例 ────────────────────────────────────────────────────────

type instance struct {
	once     sync.Once
	validate *validator.Validate
	trans    unTrans.Translator
	initErr  error
}

func (ins *instance) init() error {
	ins.once.Do(func() {
		v := validator.New()
		uni := unTrans.New(zh_Hans_CN.New())

		trans, found := uni.GetTranslator("zh_Hans_CN")
		if !found {
			ins.initErr = fmt.Errorf("validate: translator zh_Hans_CN not found")
			return
		}
		if err := zhTrans.RegisterDefaultTranslations(v, trans); err != nil {
			ins.initErr = fmt.Errorf("validate: register translations: %w", err)
			return
		}

		v.RegisterTagNameFunc(func(f reflect.StructField) string {
			if label := f.Tag.Get("label"); label != "" {
				return label
			}
			if tag := f.Tag.Get("json"); tag != "" && tag != "-" {
				return tag
			}
			return f.Name
		})

		ins.validate = v
		ins.trans = trans
	})
	return ins.initErr
}

func (ins *instance) check(data any) error {
	if err := ins.init(); err != nil {
		return err
	}
	err := ins.validate.Struct(data)
	if err == nil {
		return nil
	}
	var ve validator.ValidationErrors
	if !errors.As(err, &ve) {
		return err
	}
	result := make(Errors, 0, len(ve))
	for _, fe := range ve {
		result = append(result, &FieldError{
			Field:   fe.Field(),
			Message: fe.Translate(ins.trans),
		})
	}
	return result
}

func (ins *instance) registerValidation(tag string, fn validator.Func) error {
	if err := ins.init(); err != nil {
		return err
	}
	return ins.validate.RegisterValidation(tag, fn)
}

var std = &instance{}

// ── 公开 API ────────────────────────────────────────────────────────

// Check 校验结构体，通过返回 nil，失败返回 Errors
func Check(data any) error {
	return std.check(data)
}

// RegisterValidation 注册自定义校验规则
func RegisterValidation(tag string, fn validator.Func) error {
	return std.registerValidation(tag, fn)
}