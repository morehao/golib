package storagetest_test

import (
	"testing"

	"github.com/morehao/golib/gtest/storagetest"
	"github.com/morehao/golib/storage"
)

// TestContractAPIShape 冻结 storagetest 契约的调用形态。
func TestContractAPIShape(t *testing.T) {
	_ = func(t testing.TB, s storage.Storage, pb storage.PathBuilder) {
		spec := storagetest.Spec{S: s, Bucket: "testbucket"}
		_ = storagetest.StorageCases()
		_ = storagetest.PresignLiveCases()
		_ = func(tt *testing.T) {
			storagetest.RunCases(tt, spec, storagetest.StorageCases()...)
		}
		_ = storagetest.NewMem(pb)
		f := storagetest.NewFakeS3()
		_ = f.URL()
		f.Close()
	}
}
