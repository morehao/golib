package local

import (
	"github.com/morehao/golib/internal/testkit"
	"github.com/morehao/golib/storage"

	"os"
	"testing"
)

func TestIntegration(t *testing.T) {
	dir := t.TempDir()
	cfg := storage.Config{
		BaseDir: dir,
		BaseURL: "http://localhost:8080/files",
	}
	s, err := New(cfg)
	if err != nil {
		t.Fatalf("New local driver: %v", err)
	}
	bucket := "testbucket"
	dataDir := dir + "/data/" + bucket
	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		t.Fatalf("create bucket dir: %v", err)
	}
	testkit.RunSuite(t, s, bucket)
}
