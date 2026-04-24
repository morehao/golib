package configkv

import (
	"testing"

	"github.com/BurntSushi/toml"
	"gopkg.in/yaml.v3"
)

func TestJSONCodec(t *testing.T) {
	codec := JSONCodec{}

	type TestStruct struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}

	obj := TestStruct{Name: "test", Age: 123}
	data, err := codec.Marshal(obj)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var result TestStruct
	if err := codec.Unmarshal(data, &result); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if result.Name != "test" || result.Age != 123 {
		t.Errorf("expected {test 123}, got %+v", result)
	}

	if codec.Name() != "json" {
		t.Errorf("expected name 'json', got '%s'", codec.Name())
	}
}

func TestTOMLCodec(t *testing.T) {
	codec := TOMLCodec{}

	type TestStruct struct {
		Name string `toml:"name"`
		Age  int    `toml:"age"`
	}

	obj := TestStruct{Name: "toml_test", Age: 456}
	data, err := codec.Marshal(obj)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var result TestStruct
	if err := toml.Unmarshal(data, &result); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if result.Name != "toml_test" || result.Age != 456 {
		t.Errorf("expected {toml_test 456}, got %+v", result)
	}

	if codec.Name() != "toml" {
		t.Errorf("expected name 'toml', got '%s'", codec.Name())
	}
}

func TestYAMLCodec(t *testing.T) {
	codec := YAMLCodec{}

	type TestStruct struct {
		Name string `yaml:"name"`
		Age  int    `yaml:"age"`
	}

	obj := TestStruct{Name: "yaml_test", Age: 789}
	data, err := codec.Marshal(obj)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var result TestStruct
	if err := yaml.Unmarshal(data, &result); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if result.Name != "yaml_test" || result.Age != 789 {
		t.Errorf("expected {yaml_test 789}, got %+v", result)
	}

	if codec.Name() != "yaml" {
		t.Errorf("expected name 'yaml', got '%s'", codec.Name())
	}
}

func TestCodecRegistry(t *testing.T) {
	registry := map[ValueType]Codec{
		ValueTypeJson: &JSONCodec{},
		ValueTypeToml: &TOMLCodec{},
		ValueTypeYaml: &YAMLCodec{},
	}

	type TestStruct struct {
		Name string `json:"name" toml:"name" yaml:"name"`
		Age  int    `json:"age" toml:"age" yaml:"age"`
	}

	obj := TestStruct{Name: "registry_test", Age: 999}

	testCases := []struct {
		valueType ValueType
	}{
		{ValueTypeJson},
		{ValueTypeToml},
		{ValueTypeYaml},
	}

	for _, tc := range testCases {
		t.Run(string(tc.valueType), func(t *testing.T) {
			codec := registry[tc.valueType]
			if codec == nil {
				t.Fatalf("no codec for %s", tc.valueType)
			}

			data, err := codec.Marshal(obj)
			if err != nil {
				t.Fatalf("marshal failed: %v", err)
			}

			var result TestStruct
			if err := codec.Unmarshal(data, &result); err != nil {
				t.Fatalf("unmarshal failed: %v", err)
			}

			if result.Name != "registry_test" || result.Age != 999 {
				t.Errorf("expected {registry_test 999}, got %+v", result)
			}
		})
	}
}

func TestAESCrypto(t *testing.T) {
	crypto, err := newAESCrypto()
	if err != nil {
		t.Fatalf("failed to create crypto: %v", err)
	}

	plaintext := "hello world"
	ciphertext, err := crypto.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("encrypt failed: %v", err)
	}

	if ciphertext == plaintext {
		t.Error("ciphertext should differ from plaintext")
	}

	decrypted, err := crypto.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("decrypt failed: %v", err)
	}

	if decrypted != plaintext {
		t.Errorf("expected '%s', got '%s'", plaintext, decrypted)
	}
}

func TestMarshalValueByType(t *testing.T) {
	registry := map[ValueType]Codec{
		ValueTypeJson: &JSONCodec{},
		ValueTypeToml: &TOMLCodec{},
		ValueTypeYaml: &YAMLCodec{},
	}
	c, _ := newAESCrypto()
	s := newStore(nil, registry, c)

	type TestStruct struct {
		Name string `json:"name" toml:"name" yaml:"name"`
	}

	obj := TestStruct{Name: "marshal_test"}

	testCases := []struct {
		valueType ValueType
		val       any
	}{
		{ValueTypeJson, obj},
		{ValueTypeToml, obj},
		{ValueTypeYaml, obj},
		{ValueTypeString, "hello"},
		{ValueTypeInt, 42},
		{ValueTypeBool, true},
	}

	for _, tc := range testCases {
		t.Run(string(tc.valueType), func(t *testing.T) {
			value, encrypted, err := s.marshalValue(tc.valueType, tc.val)
			if err != nil {
				t.Fatalf("marshalValue failed: %v", err)
			}
			if encrypted {
				t.Error("should not be encrypted for non-secret type")
			}
			if value == "" {
				t.Error("value should not be empty")
			}
		})
	}
}