package gsetting

import (
	"encoding/json"
	"strconv"

	"github.com/BurntSushi/toml"
	"gopkg.in/yaml.v3"
)

var validValueTypes = map[ValueType]bool{
	ValueTypeJson:   true,
	ValueTypeToml:   true,
	ValueTypeYaml:   true,
	ValueTypeString: true,
	ValueTypeInt:    true,
	ValueTypeBool:   true,
	ValueTypeFloat:  true,
}

func validateValueType(vt string) error {
	if !validValueTypes[ValueType(vt)] {
		return errValueTypeInvalid
	}
	return nil
}

func validateValue(valueType ValueType, value string) error {
	if value == "" {
		return errValueEmpty
	}

	switch valueType {
	case ValueTypeString:
		return nil
	case ValueTypeInt:
		if _, err := strconv.ParseInt(value, 10, 64); err != nil {
			return errValueTypeInvalid
		}
		return nil
	case ValueTypeFloat:
		if _, err := strconv.ParseFloat(value, 64); err != nil {
			return errValueTypeInvalid
		}
		return nil
	case ValueTypeBool:
		if _, err := strconv.ParseBool(value); err != nil {
			return errValueTypeInvalid
		}
		return nil
	case ValueTypeJson:
		if !json.Valid([]byte(value)) {
			return errValueTypeInvalid
		}
		return nil
	case ValueTypeYaml:
		if err := yaml.Unmarshal([]byte(value), &struct{}{}); err != nil {
			return errValueTypeInvalid
		}
		return nil
	case ValueTypeToml:
		if err := toml.Unmarshal([]byte(value), &struct{}{}); err != nil {
			return errValueTypeInvalid
		}
		return nil
	default:
		return errUnsupportedValueType
	}
}

// validateStatus 校验配置项状态只允许 enabled / disabled。
//
// 空字符串表示「未指定」：Create 默认启用、Update 表示不改，由调用方处理。
func validateStatus(status string) error {
	if status == "" {
		return nil
	}
	switch Status(status) {
	case StatusEnabled, StatusDisabled:
		return nil
	default:
		return errInvalidStatus
	}
}
