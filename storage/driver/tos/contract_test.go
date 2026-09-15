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
func TestProfile(t *testing.T) {
	if !profile.ForcePathStyle {
		t.Error("TOS requires path-style addressing")
	}
	if profile.ConditionalWrite != storage.ConditionalWriteNativeIfNoneMatch {
		t.Errorf("ConditionalWrite = %v, want native_if_none_match", profile.ConditionalWrite)
	}
	if profile.Name != string(storage.DriverTOS) {
		t.Errorf("Name = %q, want %q", profile.Name, storage.DriverTOS)
	}
	if profile.Limits != (storage.Limits{}) {
		t.Errorf("Limits = %+v; 零值表示沿用 s3base.S3Limits，不应在这里重复声明", profile.Limits)
	}
}
