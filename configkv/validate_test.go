package configkv

import "testing"

func TestValidateValueType(t *testing.T) {
	testCases := []struct {
		vt   string
		want error
	}{
		{"string", nil},
		{"int", nil},
		{"float", nil},
		{"bool", nil},
		{"json", nil},
		{"yaml", nil},
		{"toml", nil},
		{"invalid", errValueTypeInvalid},
		{"", errValueTypeInvalid},
	}

	for _, tc := range testCases {
		t.Run(tc.vt, func(t *testing.T) {
			err := validateValueType(tc.vt)
			if err != tc.want {
				t.Errorf("validateValueType(%q) = %v, want %v", tc.vt, err, tc.want)
			}
		})
	}
}

func TestValidateValue(t *testing.T) {
	testCases := []struct {
		name string
		vt   ValueType
		val  string
		want error
	}{
		{"string valid", ValueTypeString, "hello", nil},
		{"string empty", ValueTypeString, "", errValueEmpty},
		{"int valid", ValueTypeInt, "42", nil},
		{"int negative", ValueTypeInt, "-1", nil},
		{"int invalid", ValueTypeInt, "abc", errValueTypeInvalid},
		{"int empty", ValueTypeInt, "", errValueEmpty},
		{"float valid", ValueTypeFloat, "3.14", nil},
		{"float invalid", ValueTypeFloat, "notfloat", errValueTypeInvalid},
		{"float empty", ValueTypeFloat, "", errValueEmpty},
		{"bool true", ValueTypeBool, "true", nil},
		{"bool false", ValueTypeBool, "false", nil},
		{"bool invalid", ValueTypeBool, "maybe", errValueTypeInvalid},
		{"bool empty", ValueTypeBool, "", errValueEmpty},
		{"json valid object", ValueTypeJson, `{"key":"value"}`, nil},
		{"json valid array", ValueTypeJson, `[1,2,3]`, nil},
		{"json invalid", ValueTypeJson, `{invalid}`, errValueTypeInvalid},
		{"json empty", ValueTypeJson, "", errValueEmpty},
		{"yaml valid", ValueTypeYaml, "key: value", nil},
		{"yaml invalid", ValueTypeYaml, "[invalid\n  bad", errValueTypeInvalid},
		{"yaml empty", ValueTypeYaml, "", errValueEmpty},
		{"toml valid", ValueTypeToml, `key = "value"`, nil},
		{"toml invalid", ValueTypeToml, `[[invalid`, errValueTypeInvalid},
		{"toml empty", ValueTypeToml, "", errValueEmpty},
		{"unsupported type", ValueType("unknown"), "value", errUnsupportedValueType},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateValue(tc.vt, tc.val)
			if err != tc.want {
				t.Errorf("validateValue(%q, %q) = %v, want %v", tc.vt, tc.val, err, tc.want)
			}
		})
	}
}