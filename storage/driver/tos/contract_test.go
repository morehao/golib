package tos

import (
	"testing"

	"github.com/morehao/golib/storage"
	"github.com/morehao/golib/storage/driver/s3base"
)

func TestContract(t *testing.T) {
	var _ storage.Storage = (*s3base.Driver)(nil)
}

// TestProfile 钉住本 provider 声明的差异（详见 s3base.ProviderProfile 的说明）。
// 寻址风格与 MinPartSize 两条是 2026-09-15 对 tos-s3-cn-beijing.volces.com
// 真实端点的实测结论，不是照抄 S3 的默认行为。
func TestProfile(t *testing.T) {
	if profile.ForcePathStyle {
		t.Error("TOS 只接受三级域名寻址：path-style 实测回 403 InvalidPathAccess")
	}
	if profile.ConditionalWrite != storage.ConditionalWriteNativeIfNoneMatch {
		t.Errorf("ConditionalWrite = %v, want native_if_none_match", profile.ConditionalWrite)
	}
	if profile.Name != string(storage.DriverTOS) {
		t.Errorf("Name = %q, want %q", profile.Name, storage.DriverTOS)
	}
	// 非末片下限实测为 4 MiB（二进制）：4,194,303 字节被 EntityTooSmall 拒绝，
	// 4,194,304 通过。填 5 MiB 会让客户端拒掉后端本可接受的上传。
	if profile.Limits.MinPartSize != 4<<20 {
		t.Errorf("MinPartSize = %d, want %d（4 MiB：非末片 4,194,303 字节实测被 EntityTooSmall 拒绝）",
			profile.Limits.MinPartSize, 4<<20)
	}
	// 除 MinPartSize 外必须与 S3Limits 同源，避免两处各写一遍而漂移。
	want := s3base.S3Limits
	want.MinPartSize = profile.Limits.MinPartSize
	if profile.Limits != want {
		t.Errorf("Limits = %+v, want %+v（除 MinPartSize 外应与 s3base.S3Limits 一致）", profile.Limits, want)
	}
}
