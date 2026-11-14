package gutil

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestZipDirAndUnzip(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "srcdir")
	require.NoError(t, os.MkdirAll(filepath.Join(src, "sub"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(src, "a.txt"), []byte("hello"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(src, "sub", "b.txt"), []byte("world"), 0o644))

	zipPath := filepath.Join(tmp, "out.zip")
	require.NoError(t, ZipDir(src, zipPath))

	// 校验 zip 内条目以 srcDir 基名为根
	zr, err := zip.OpenReader(zipPath)
	require.NoError(t, err)
	names := map[string]bool{}
	for _, f := range zr.File {
		names[f.Name] = true
	}
	zr.Close()
	require.True(t, names["srcdir/a.txt"], names)
	require.True(t, names["srcdir/sub/b.txt"], names)

	// 解压并校验内容
	dest := filepath.Join(tmp, "dest")
	require.NoError(t, Unzip(zipPath, dest))
	got, err := os.ReadFile(filepath.Join(dest, "srcdir", "a.txt"))
	require.NoError(t, err)
	require.Equal(t, "hello", string(got))
	got, err = os.ReadFile(filepath.Join(dest, "srcdir", "sub", "b.txt"))
	require.NoError(t, err)
	require.Equal(t, "world", string(got))
}

func TestZipFiles(t *testing.T) {
	tmp := t.TempDir()
	f1 := filepath.Join(tmp, "one.txt")
	f2 := filepath.Join(tmp, "two.txt")
	require.NoError(t, os.WriteFile(f1, []byte("1"), 0o644))
	require.NoError(t, os.WriteFile(f2, []byte("2"), 0o644))

	zipPath := filepath.Join(tmp, "files.zip")
	require.NoError(t, ZipFiles(zipPath, f1, f2))

	dest := filepath.Join(tmp, "dest")
	require.NoError(t, Unzip(zipPath, dest))
	b, err := os.ReadFile(filepath.Join(dest, "one.txt"))
	require.NoError(t, err)
	require.Equal(t, "1", string(b))
}

func TestZipErrors(t *testing.T) {
	tmp := t.TempDir()
	require.Error(t, ZipDir(filepath.Join(tmp, "nope"), filepath.Join(tmp, "x.zip")))

	f := filepath.Join(tmp, "file.txt")
	require.NoError(t, os.WriteFile(f, []byte("x"), 0o644))
	// ZipDir 传入文件而非目录应报错
	require.Error(t, ZipDir(f, filepath.Join(tmp, "x.zip")))
	// ZipFiles 传入不存在文件应报错
	require.Error(t, ZipFiles(filepath.Join(tmp, "y.zip"), filepath.Join(tmp, "missing")))
}

func TestUnzipRejectsTraversal(t *testing.T) {
	tmp := t.TempDir()
	zipPath := filepath.Join(tmp, "evil.zip")
	out, err := os.Create(zipPath)
	require.NoError(t, err)
	zw := zip.NewWriter(out)
	w, err := zw.Create("../evil.txt")
	require.NoError(t, err)
	_, _ = w.Write([]byte("x"))
	require.NoError(t, zw.Close())
	out.Close()

	err = Unzip(zipPath, filepath.Join(tmp, "dest"))
	require.Error(t, err)
}
