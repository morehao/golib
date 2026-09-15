package storage

import (
	"errors"
	"testing"
)

// s3Caps 是 S3 系列后端的限制，用于校验断言。
func s3Caps() Caps {
	return Caps{
		Multipart: true,
		Limits: Limits{
			MaxSinglePut:   5 << 30,
			MinPartSize:    5 << 20,
			MaxParts:       10000,
			MaxDeleteBatch: 1000,
			MaxListPage:    1000,
		},
	}
}

func TestValidateParts_OK(t *testing.T) {
	caps := s3Caps()
	ok := [][]PartInfo{
		{{PartNumber: 1, Size: 5 << 20}, {PartNumber: 2, Size: 5 << 20}, {PartNumber: 3, Size: 1}},
		{{PartNumber: 1, Size: 5 << 20}},                  // 单片
		{{PartNumber: 1, Size: 1}},                        // 单片可以小于 MinPartSize（它就是末片）
		{{PartNumber: 1}, {PartNumber: 2}},                // Size 未知：跳过大小校验
		{{PartNumber: 1, Size: 5 << 20}, {PartNumber: 2}}, // 末片大小未知
	}
	for i, parts := range ok {
		if err := ValidateParts(parts, caps); err != nil {
			t.Errorf("case %d: 期望通过, got %v", i, err)
		}
	}
}

func TestValidateParts_Errors(t *testing.T) {
	caps := s3Caps()
	cases := []struct {
		name  string
		parts []PartInfo
	}{
		{"part number zero", []PartInfo{{PartNumber: 0, Size: 5 << 20}}},
		{"part number negative", []PartInfo{{PartNumber: -1, Size: 5 << 20}}},
		{"part number over max", []PartInfo{{PartNumber: 10001, Size: 5 << 20}}},
		{"descending order", []PartInfo{{PartNumber: 2, Size: 5 << 20}, {PartNumber: 1, Size: 5 << 20}}},
		{"duplicate part number", []PartInfo{{PartNumber: 1, Size: 5 << 20}, {PartNumber: 1, Size: 5 << 20}}},
		{"negative size", []PartInfo{{PartNumber: 1, Size: -1}}},
		// 非末片小于 MinPartSize 能在 complete 阶段判定 —— 这条是可判定的。
		{"non-final part below min", []PartInfo{{PartNumber: 1, Size: 1 << 20}, {PartNumber: 2, Size: 1 << 20}}},
		{"part over max single put", []PartInfo{{PartNumber: 1, Size: (5 << 30) + 1}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateParts(c.parts, caps)
			if err == nil {
				t.Fatal("期望报错, got nil")
			}
			if !errors.Is(err, ErrInvalidArgument) {
				t.Errorf("错误必须包装 ErrInvalidArgument, got %v", err)
			}
		})
	}
}

// 超过 MaxParts 片必须在本地就拒绝，而不是等后端报错。
func TestValidateParts_TooManyParts(t *testing.T) {
	caps := s3Caps()
	caps.Limits.MaxParts = 3
	parts := []PartInfo{
		{PartNumber: 1, Size: 5 << 20},
		{PartNumber: 2, Size: 5 << 20},
		{PartNumber: 3, Size: 5 << 20},
		{PartNumber: 4, Size: 5 << 20},
	}
	if err := ValidateParts(parts, caps); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("期望 ErrInvalidArgument, got %v", err)
	}
}

// local 声明 Limits 全 0（无限制），此时只保留与限制无关的检查。
func TestValidateParts_UnlimitedBackend(t *testing.T) {
	caps := Caps{Multipart: true}
	// 无限制后端下，任意小的非末片都合法。
	if err := ValidateParts([]PartInfo{{PartNumber: 1, Size: 1}, {PartNumber: 2, Size: 1}}, caps); err != nil {
		t.Errorf("无限制后端不应因大小报错: %v", err)
	}
	// 分片号与顺序是与限制无关的结构性约束，仍然生效。
	if err := ValidateParts([]PartInfo{{PartNumber: 0}}, caps); err == nil {
		t.Error("分片号 >= 1 与后端限制无关，必须仍然生效")
	}
	if err := ValidateParts([]PartInfo{{PartNumber: 2}, {PartNumber: 1}}, caps); err == nil {
		t.Error("升序要求与后端限制无关，必须仍然生效")
	}
}

func TestValidatePartCount(t *testing.T) {
	caps := s3Caps()
	if err := ValidatePartCount(1, caps); err != nil {
		t.Errorf("part 1: %v", err)
	}
	if err := ValidatePartCount(10000, caps); err != nil {
		t.Errorf("part 10000: %v", err)
	}
	if err := ValidatePartCount(0, caps); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("part 0: %v", err)
	}
	if err := ValidatePartCount(10001, caps); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("part 10001: %v", err)
	}
}

func TestFromPutOptions(t *testing.T) {
	in := FromPutOptions(
		WithContentType("text/plain"),
		WithMetadata(map[string]string{"a": "b"}),
		WithStorageClass("STANDARD_IA"),
	)
	if in.ContentType != "text/plain" {
		t.Errorf("ContentType = %q", in.ContentType)
	}
	if in.Metadata["a"] != "b" {
		t.Errorf("Metadata = %v", in.Metadata)
	}
	if in.StorageClass != "STANDARD_IA" {
		t.Errorf("StorageClass = %q", in.StorageClass)
	}
	// nil option 不得 panic。
	_ = FromPutOptions(nil, WithContentType("x"))
}
