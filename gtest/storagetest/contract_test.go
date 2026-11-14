package storagetest_test

import (
	"testing"

	"github.com/morehao/golib/gtest/storagetest"
	"github.com/morehao/golib/storage"
	"github.com/morehao/golib/storage/driver/s3base"
)

// newStubStorage 构造一个指向进程内 FakeS3 的 s3base driver（storage.Storage）。
//
// 这正是 s3base/conformance_test.go 里 newStubDriver 的做法：s3base 是通常
// 消费 FakeS3 的驱动，因此它也最适合作为 FakeS3 的适配器。这里把它复制进
// 本测试（而不是依赖 internal/testutil），从而自证 FakeS3 + 契约是自洽的。
func newStubStorage(t *testing.T, f *storagetest.FakeS3) storage.Storage {
	t.Helper()
	cfg := storage.Config{
		Endpoint:  f.URL(),
		Region:    "us-east-1",
		AccessKey: "test-ak",
		SecretKey: "test-sk",
	}
	pb := &storage.S3PathBuilder{}
	s, err := s3base.New(cfg, pb, s3base.WithProfile(s3base.ProviderProfile{
		Name:             "fakes3",
		ForcePathStyle:   true,
		ConditionalWrite: storage.ConditionalWriteNativeIfNoneMatch,
		Limits: storage.Limits{
			MaxSinglePut:   5 << 30,
			MinPartSize:    1,
			MaxParts:       10000,
			MaxDeleteBatch: 1000,
			MaxListPage:    1000,
		},
	}))
	if err != nil {
		t.Fatalf("s3base.New: %v", err)
	}
	return s
}

// TestContract_FakeS3 用进程内 S3 桩跑完整契约套件，证明 FakeS3 + StorageCases 自洽。
func TestContract_FakeS3(t *testing.T) {
	f := storagetest.NewFakeS3()
	// 用 t.Cleanup（LIFO）而不是 defer：契约已注册清理，若 defer 先关桩，
	// 清理请求会拿到 connection refused。
	t.Cleanup(f.Close)
	storagetest.RunCases(t, storagetest.Spec{
		S:      newStubStorage(t, f),
		Bucket: "stubbucket",
	}, storagetest.StorageCases()...)
}

// TestContract_Mem 用完整语义内存实现跑完整契约套件，证明 NewMem 不是空壳。
func TestContract_Mem(t *testing.T) {
	s := storagetest.NewMem(&storage.S3PathBuilder{})
	storagetest.RunCases(t, storagetest.Spec{
		S:      s,
		Bucket: "b",
	}, storagetest.StorageCases()...)
}
