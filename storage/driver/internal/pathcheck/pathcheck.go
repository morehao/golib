// Package pathcheck 提供 bucket 与 object key 的命名校验。
package pathcheck

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/morehao/golib/storage"
)

var bucketRegex = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,61}[a-z0-9]$`)

// ValidateBucket 校验 bucket 名称（S3 命名规则）。
func ValidateBucket(bucket string) error {
	if bucket == "" {
		return fmt.Errorf("%w: bucket is empty", storage.ErrInvalidPath)
	}
	if !bucketRegex.MatchString(bucket) {
		return fmt.Errorf("%w: invalid bucket %q", storage.ErrInvalidPath, bucket)
	}
	return nil
}

// ValidateKey 校验 object key 名称。
func ValidateKey(key string) error {
	if key == "" {
		return fmt.Errorf("%w: key is empty", storage.ErrInvalidPath)
	}
	if strings.HasPrefix(key, "/") {
		return fmt.Errorf("%w: key %q must not start with /", storage.ErrInvalidPath, key)
	}
	if strings.Contains(key, "..") {
		return fmt.Errorf("%w: key %q must not contain ..", storage.ErrInvalidPath, key)
	}
	if strings.Contains(key, "//") {
		return fmt.Errorf("%w: key %q must not contain //", storage.ErrInvalidPath, key)
	}
	return nil
}
