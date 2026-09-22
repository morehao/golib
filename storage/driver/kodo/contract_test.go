package kodo

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/morehao/golib/storage"
	"github.com/morehao/golib/storage/driver/s3base"
)

func TestContract(t *testing.T) {
	var _ storage.Storage = (*s3base.Driver)(nil)
}

// TestProfile 钉住本 provider 声明的差异：每一条都对应一次真机实测（2026-09-22，
// cn-north-1 / 桶 local-sh）或官方文档，取值理由见 driver.go 中 profile 的注释。
func TestProfile(t *testing.T) {
	if profile.Name != string(storage.DriverKodo) {
		t.Errorf("Name = %q, want %q", profile.Name, storage.DriverKodo)
	}
	// 官方同时支持 path-style 与 virtual-hosted；选 path-style 是因为
	// S3 空间名可能含 '.'，通配证书 *.s3.<region>.qiniucs.com 匹配不上。
	if !profile.ForcePathStyle {
		t.Error("Kodo 应使用 path-style：virtual-hosted 在含 '.' 的 S3 空间名下会 TLS 失败")
	}
	// 条件写未获支持且会静默覆盖：2026-09-22 实测连发两次 If-None-Match:* 都成功、
	// 内容被后者覆盖。把声明改强之前必须先有一条真机用例证明它生效，
	// 否则会静默退化为覆盖写（本仓库明令禁止）。
	if profile.ConditionalWrite != storage.ConditionalWriteNone {
		t.Errorf("ConditionalWrite = %v, want none（实测该头既不报错也不生效，会静默覆盖）",
			profile.ConditionalWrite)
	}
	// 厂商私有错误码必须显式映射，不依赖 HTTP 状态码兜底。
	for code, want := range map[string]storage.Kind{
		"IncorrectRegion":     storage.KindInvalidArgument, // 区域与桶不匹配（实测 400）
		"InvalidStorageClass": storage.KindInvalidArgument, // 如误用 S3 的 STANDARD_IA（实测 400）
	} {
		if got := profile.ErrorCodeKind[code]; got != want {
			t.Errorf("ErrorCodeKind[%s] = %v, want %v", code, got, want)
		}
	}
	// 分片下限取官方分片上传 v2 文档的 1 MiB，而非照抄 S3 的 5 MiB。
	if profile.Limits.MinPartSize != 1<<20 {
		t.Errorf("MinPartSize = %d, want %d（七牛分片 v2：非末片 1MB - 1GB）",
			profile.Limits.MinPartSize, 1<<20)
	}
	if profile.Limits.MaxParts != 10000 {
		t.Errorf("MaxParts = %d, want 10000（七牛分片 v2：最多 10000 个 Part）", profile.Limits.MaxParts)
	}
	// 除 MinPartSize/MaxParts 外必须与 S3Limits 同源，避免两处各写一遍而漂移。
	want := s3base.S3Limits
	want.MinPartSize = profile.Limits.MinPartSize
	want.MaxParts = profile.Limits.MaxParts
	if profile.Limits != want {
		t.Errorf("Limits = %+v, want %+v（除 MinPartSize/MaxParts 外应与 s3base.S3Limits 一致）",
			profile.Limits, want)
	}
}

// TestConditionalWriteRefused 把"Kodo 不支持条件写"变成可回归的断言，且不需要凭据：
// s3base 的拒绝发生在任何网络调用之前（s3driver.go:380），因此用假凭据构建驱动即可。
//
// 这条用例的存在意义：契约套件的 ConditionalWrite 子测试在声明 None 时被跳过，
// 跳过本身不构成保护；真正要钉住的是"声明 None ⇒ 显式拒绝，绝不静默覆盖"。
func TestConditionalWriteRefused(t *testing.T) {
	s, err := New(storage.Config{
		Endpoint:  "https://s3.cn-north-1.qiniucs.com",
		Region:    "cn-north-1",
		AccessKey: "fake-ak",
		SecretKey: "fake-sk",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = s.PutObject(context.Background(), "local-sh", "k",
		bytes.NewReader([]byte("x")), storage.WithIfNotExists())
	if !errors.Is(err, storage.ErrNotSupported) {
		t.Fatalf("WithIfNotExists = %v, want ErrNotSupported（声明 None 时必须显式拒绝，不得退化为覆盖写）", err)
	}
}
