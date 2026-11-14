package gutil

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ZipDir 将 srcDir 目录（含子目录）打包为 zipPath 指定的 zip 文件。
// zip 内的路径以 srcDir 的基名为根，例如 srcDir=/a/b 时条目形如 b/x/y.txt。
// zipPath 已存在时会被覆盖；其所在目录需已存在。
func ZipDir(srcDir, zipPath string) error {
	info, err := os.Stat(srcDir)
	if err != nil {
		return fmt.Errorf("gutil: stat src dir: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("gutil: %q is not a directory", srcDir)
	}

	out, err := os.Create(zipPath)
	if err != nil {
		return fmt.Errorf("gutil: create zip: %w", err)
	}
	defer out.Close()

	zw := zip.NewWriter(out)
	base := filepath.Base(filepath.Clean(srcDir))

	walkErr := filepath.Walk(srcDir, func(path string, fi os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		name := filepath.ToSlash(filepath.Join(base, rel))
		if fi.IsDir() {
			// 目录条目以 / 结尾，便于解压端还原空目录
			_, err := zw.Create(name + "/")
			return err
		}
		return writeZipFile(zw, path, name, fi)
	})
	if walkErr != nil {
		_ = zw.Close()
		return walkErr
	}
	return zw.Close()
}

// ZipFiles 将若干文件打包为 zipPath 指定的 zip 文件。
// 条目名规则：相对路径保留其（清理后的）相对结构；绝对路径只取文件名（基名）。
// 传入不存在的文件或目录会返回错误。
func ZipFiles(zipPath string, files ...string) error {
	out, err := os.Create(zipPath)
	if err != nil {
		return fmt.Errorf("gutil: create zip: %w", err)
	}
	defer out.Close()

	zw := zip.NewWriter(out)
	for _, f := range files {
		fi, err := os.Stat(f)
		if err != nil {
			_ = zw.Close()
			return fmt.Errorf("gutil: stat %q: %w", f, err)
		}
		if fi.IsDir() {
			_ = zw.Close()
			return fmt.Errorf("gutil: %q is a directory, use ZipDir", f)
		}
		var name string
		if filepath.IsAbs(f) {
			name = filepath.Base(f)
		} else {
			name = filepath.ToSlash(filepath.Clean(f))
		}
		if err := writeZipFile(zw, f, name, fi); err != nil {
			_ = zw.Close()
			return err
		}
	}
	return zw.Close()
}

// Unzip 将 zipPath 指定的 zip 文件解压到 destDir。
// destDir 不存在时会自动创建。解压前会校验条目路径，拒绝目录穿越（../）条目。
func Unzip(zipPath, destDir string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("gutil: open zip: %w", err)
	}
	defer zr.Close()

	cleanDest := filepath.Clean(destDir)
	if err := os.MkdirAll(cleanDest, os.ModePerm); err != nil {
		return fmt.Errorf("gutil: create dest dir: %w", err)
	}

	for _, f := range zr.File {
		target := filepath.Join(cleanDest, filepath.FromSlash(f.Name))
		// 防目录穿越：目标必须仍在 destDir 之内
		if !strings.HasPrefix(target, cleanDest+string(os.PathSeparator)) && target != cleanDest {
			return fmt.Errorf("gutil: illegal file path in zip: %q", f.Name)
		}

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, os.ModePerm); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), os.ModePerm); err != nil {
			return err
		}
		if err := extractZipFile(f, target); err != nil {
			return err
		}
	}
	return nil
}

func writeZipFile(zw *zip.Writer, srcPath, name string, fi os.FileInfo) error {
	w, err := zw.Create(name)
	if err != nil {
		return err
	}
	f, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(w, f)
	return err
}

func extractZipFile(f *zip.File, target string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()

	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, rc)
	return err
}
