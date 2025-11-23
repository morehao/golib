package gutil

import (
	"os"
	"path/filepath"
	"strings"
)

// GetFileExtension 获取文件扩展名
func GetFileExtension(name string) string {
	if len(name) == 0 {
		return ""
	}
	return filepath.Ext(name)
}

// TrimFileExtension 去除文件扩展名
func TrimFileExtension(name string) string {
	if len(name) == 0 {
		return ""
	}
	fileExt := filepath.Ext(name)
	return strings.TrimSuffix(name, fileExt)
}

// FileExists 检查文件是否存在
func FileExists(filepath string) bool {
	if _, err := os.Stat(filepath); os.IsNotExist(err) {
		return false
	}
	return true
}

func PathExists(path string) (bool, error) {
	fi, err := os.Stat(path)
	if err == nil {
		if fi.IsDir() {
			return true, nil
		}
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func CreateDir(dir string) error {
	if exist := FileExists(dir); !exist {
		if err := os.MkdirAll(dir, os.ModePerm); err != nil {
			return err
		}
	}
	return nil
}
