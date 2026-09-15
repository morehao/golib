package minio

import (
	"testing"

	"github.com/morehao/golib/storage"
	"github.com/morehao/golib/storage/driver/s3base"
)

func TestContract(t *testing.T) {
	var _ storage.Storage = (*s3base.Driver)(nil)
}

// TestProfile 钉住本 provider 声明的差异。这些差异原先表达为共享基类里对
// endpoint 域名的嗅探，现在必须由本包的数据表显式声明，因此需要测试锁住。
func TestProfile(t *testing.T) {
	if !profile.ForcePathStyle {
		t.Error("MinIO requires path-style addressing")
	}
	if profile.ConditionalWrite != storage.ConditionalWriteNativeIfNoneMatch {
		t.Errorf("ConditionalWrite = %v, want native_if_none_match", profile.ConditionalWrite)
	}
	if profile.Name != string(storage.DriverMinio) {
		t.Errorf("Name = %q, want %q", profile.Name, storage.DriverMinio)
	}
	if profile.Limits != (storage.Limits{}) {
		t.Errorf("Limits = %+v; 零值表示沿用 s3base.S3Limits，不应在这里重复声明", profile.Limits)
	}
}
