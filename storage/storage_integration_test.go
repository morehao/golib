package storage_test

import (
	"testing"

	"github.com/morehao/golib/storage"
	_ "github.com/morehao/golib/storage/driver/minio"
	"github.com/morehao/golib/storage/testkit"
)

func TestMockSuite(t *testing.T) {
	pb := &storage.S3PathBuilder{BaseURL: "http://localhost"}
	s := testkit.NewMock(pb)
	testkit.RunSuite(t, s, "test-bucket")
}

func TestRegistryDrivers(t *testing.T) {
	drivers := storage.Drivers()
	if len(drivers) == 0 {
		t.Fatal("expected at least minio driver registered")
	}
	t.Log("registered drivers:", drivers)
}
