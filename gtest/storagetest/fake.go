package storagetest

// FakeS3 是进程内的最小 S3 协议桩。
type FakeS3 struct {
	_ struct{}
}

// NewFakeS3 启动一个进程内 S3 桩。
func NewFakeS3() *FakeS3 { panic("storagetest: NewFakeS3 not implemented") }

// URL 返回桩的基地址，可作为 storage.Config.Endpoint。
func (f *FakeS3) URL() string { panic("storagetest: FakeS3.URL not implemented") }

// Close 关闭桩。
func (f *FakeS3) Close() { panic("storagetest: FakeS3.Close not implemented") }
