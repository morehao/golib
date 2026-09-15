package oss

import (
	"testing"

	"github.com/morehao/golib/storage"
	"github.com/morehao/golib/storage/driver/s3base"
)

func TestContract(t *testing.T) {
	var _ storage.Storage = (*s3base.Driver)(nil)
}

// TestProfile 钉住本 provider 声明的差异（详见 s3base.ProviderProfile 的说明）。
// 这里的每一条都是 2026-09-15 对 oss-cn-beijing 真实端点的实测结论，
// 不是照抄 S3 的默认行为。
func TestProfile(t *testing.T) {
	if profile.ForcePathStyle {
		t.Error("OSS 只接受三级域名寻址：path-style 实测回 403 SecondLevelDomainForbidden")
	}
	if profile.ConditionalWrite != storage.ConditionalWriteVendorHeader {
		t.Errorf("ConditionalWrite = %v, want vendor_header（OSS 不支持 If-None-Match:*）", profile.ConditionalWrite)
	}
	if profile.ConditionalWriteOption == nil {
		t.Error("vendor_header 模式必须带 ConditionalWriteOption，否则条件写会静默退化为覆盖写")
	}
	if len(profile.S3Options) == 0 {
		t.Error("OSS 不支持 aws-chunked 流式校验和，必须把 RequestChecksumCalculation 降为 when_required")
	}
	if len(profile.APIOptions) == 0 {
		t.Error("OSS 的 DeleteObjects 需要 Content-MD5 中间件")
	}
	if profile.Name != string(storage.DriverOSS) {
		t.Errorf("Name = %q, want %q", profile.Name, storage.DriverOSS)
	}
	// 非末分片下限是 OSS 唯一的限制差异（100 KB，而非 S3 的 5 MiB），
	// 其余必须与 S3Limits 同源，避免两处各写一遍而漂移。
	if profile.Limits.MinPartSize != 100<<10 {
		t.Errorf("MinPartSize = %d, want %d（OSS 允许 100 KB 分片）", profile.Limits.MinPartSize, 100<<10)
	}
	want := s3base.S3Limits
	want.MinPartSize = profile.Limits.MinPartSize
	if profile.Limits != want {
		t.Errorf("Limits = %+v, want %+v（除 MinPartSize 外应与 s3base.S3Limits 一致）", profile.Limits, want)
	}
}
