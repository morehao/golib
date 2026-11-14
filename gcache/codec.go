package gcache

import "encoding/json"

// Codec 负责值 <-> []byte 的序列化。对齐 gsetting.Codec 的写法，
// 便于调用方在 gsetting 与 gcache 间复用同一套编码器。
type Codec interface {
	Marshal(v any) ([]byte, error)
	Unmarshal(data []byte, v any) error
	Name() string
}

// JSONCodec 是默认 Codec。
type JSONCodec struct{}

func (JSONCodec) Marshal(v any) ([]byte, error) { return json.Marshal(v) }

func (JSONCodec) Unmarshal(data []byte, v any) error { return json.Unmarshal(data, v) }

func (JSONCodec) Name() string { return "json" }
