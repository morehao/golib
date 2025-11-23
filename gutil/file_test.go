package gutil

import (
	"fmt"
	"testing"
)

func TestGetFileExtension(t *testing.T) {
	name := "_test.go"
	fmt.Println(GetFileExtension(name))
}

func TestTrimFileExtension(t *testing.T) {
	name := "_test.go"
	fmt.Println(TrimFileExtension(name))
}
