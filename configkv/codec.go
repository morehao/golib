package configkv

import (
	"encoding/json"

	"github.com/BurntSushi/toml"
	"gopkg.in/yaml.v3"
)

type Codec interface {
	Marshal(v any) ([]byte, error)
	Unmarshal(data []byte, v any) error
	Name() string
}

type JSONCodec struct{}

func (c JSONCodec) Marshal(v any) ([]byte, error) {
	return json.Marshal(v)
}

func (c JSONCodec) Unmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v)
}

func (c JSONCodec) Name() string {
	return "json"
}

type TOMLCodec struct{}

func (c TOMLCodec) Marshal(v any) ([]byte, error) {
	return toml.Marshal(v)
}

func (c TOMLCodec) Unmarshal(data []byte, v any) error {
	return toml.Unmarshal(data, v)
}

func (c TOMLCodec) Name() string {
	return "toml"
}

type YAMLCodec struct{}

func (c YAMLCodec) Marshal(v any) ([]byte, error) {
	return yaml.Marshal(v)
}

func (c YAMLCodec) Unmarshal(data []byte, v any) error {
	return yaml.Unmarshal(data, v)
}

func (c YAMLCodec) Name() string {
	return "yaml"
}