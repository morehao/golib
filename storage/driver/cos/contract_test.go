package cos

import (
	"testing"

	"github.com/morehao/golib/storage"
	"github.com/morehao/golib/storage/driver/s3base"
)

func TestContract(t *testing.T) {
	var _ storage.Storage = (*driver)(nil)
}

// TestProfile 钉住 COS 与 S3 协议的三处差异。删掉原先重复实现的那份
// usePathStyle 后，寻址风格与条件写机制的声明只能靠这张表，因此必须有测试。
func TestProfile(t *testing.T) {
	if profile.ForcePathStyle {
		t.Error("COS uses virtual-hosted addressing, not path-style")
	}
	if profile.ConditionalWrite != storage.ConditionalWriteVendorHeader {
		t.Errorf("ConditionalWrite = %v, want vendor_header", profile.ConditionalWrite)
	}
	if profile.ConditionalWriteOption == nil {
		t.Error("vendor_header 模式必须带 ConditionalWriteOption，否则条件写会静默退化为覆盖写")
	}
	if len(profile.APIOptions) == 0 {
		t.Error("COS 的 DeleteObjects 需要 Content-MD5 中间件")
	}
	if profile.Name != string(storage.DriverCOS) {
		t.Errorf("Name = %q, want %q", profile.Name, storage.DriverCOS)
	}
	// 分块下限实测为 1 MiB（二进制），不是文档字面的 1e6 字节。
	if profile.Limits.MinPartSize != 1<<20 {
		t.Errorf("MinPartSize = %d, want %d（1 MiB：1,000,000 字节实测被 EntityTooSmall 拒绝）",
			profile.Limits.MinPartSize, 1<<20)
	}
	want := s3base.S3Limits
	want.MinPartSize = profile.Limits.MinPartSize
	if profile.Limits != want {
		t.Errorf("Limits = %+v, want %+v（除 MinPartSize 外应与 s3base.S3Limits 一致）", profile.Limits, want)
	}
}
