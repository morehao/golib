package testkit

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/morehao/golib/storage"
)

// RunSuite 对一个 storage 实例跑通用一致性测试。
// bucket 参数指定测试用的 bucket 名称。
func RunSuite(t *testing.T, s storage.Storage, bucket string) {
	t.Helper()
	ctx := context.Background()

	t.Run("PutGet", func(t *testing.T) {
		data := []byte("hello storagetest")
		res, err := s.PutObject(ctx, bucket, "k1", bytes.NewReader(data), storage.WithContentType("text/plain"))
		if err != nil {
			t.Fatal(err)
		}
		if res.Path == nil {
			t.Fatal("Path is nil")
		}
		if res.Path.Path() == "" {
			t.Error("Path.Path() is empty")
		}

		obj, err := s.GetObject(ctx, bucket, "k1")
		if err != nil {
			t.Fatal(err)
		}
		defer obj.Body.Close()
		got, _ := io.ReadAll(obj.Body)
		if !bytes.Equal(got, data) {
			t.Errorf("body mismatch")
		}
	})

	t.Run("HeadDelete", func(t *testing.T) {
		if _, err := s.PutObject(ctx, bucket, "hd1", bytes.NewReader([]byte("x"))); err != nil {
			t.Fatal(err)
		}
		if _, err := s.HeadObject(ctx, bucket, "hd1"); err != nil {
			t.Fatalf("HeadObject = %v", err)
		}
		if err := s.DeleteObject(ctx, bucket, "hd1"); err != nil {
			t.Fatal(err)
		}
		if _, err := s.HeadObject(ctx, bucket, "hd1"); !errors.Is(err, storage.ErrNotFound) {
			t.Errorf("HeadObject after delete = %v, want ErrNotFound", err)
		}
	})

	t.Run("ListPaging", func(t *testing.T) {
		_, _ = s.PutObject(ctx, bucket, "root.txt", bytes.NewReader([]byte("0")))
		_, _ = s.PutObject(ctx, bucket, "a/1.txt", bytes.NewReader([]byte("1")))
		_, _ = s.PutObject(ctx, bucket, "a/2.txt", bytes.NewReader([]byte("2")))
		_, _ = s.PutObject(ctx, bucket, "b/1.txt", bytes.NewReader([]byte("3")))

		out, err := s.ListObjects(ctx, bucket, "")
		if err != nil {
			t.Fatal(err)
		}
		if len(out.Contents) == 0 && len(out.CommonPrefixes) == 0 {
			t.Error("expected at least one entry")
		}
	})

	t.Run("PathScheme", func(t *testing.T) {
		_, _ = s.PutObject(ctx, bucket, "p1.txt", bytes.NewReader([]byte("x")))
		h, err := s.HeadObject(ctx, bucket, "p1.txt")
		if err != nil {
			t.Fatal(err)
		}
		if h.Path == nil {
			t.Fatal("Path is nil")
		}
		if h.Path.Path() == "" {
			t.Error("Path.Path() is empty")
		}
		if h.Path.URI() == "" {
			t.Error("Path.URI() is empty")
		}
	})

	t.Run("Errors", func(t *testing.T) {
		if _, err := s.HeadObject(ctx, bucket, "nonexistent-xyz"); !errors.Is(err, storage.ErrNotFound) {
			t.Errorf("HeadObject(missing) = %v, want ErrNotFound", err)
		}
		if _, err := s.PutObject(ctx, bucket, "/bad-key", bytes.NewReader([]byte("x"))); !errors.Is(err, storage.ErrInvalidPath) {
			t.Errorf("PutObject(bad-key) = %v, want ErrInvalidPath", err)
		}
		// 重复删除应该幂等
		if err := s.DeleteObject(ctx, bucket, "nonexistent"); err != nil {
			t.Errorf("DeleteObject(nonexistent) = %v, want nil (idempotent)", err)
		}
	})
}
