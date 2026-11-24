package s3base

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
)

// TestChecksumFrom 锁定 checksumFrom 的算法优先级与新增算法支持。
func TestChecksumFrom(t *testing.T) {
	p := func(s string) *string { return aws.String(s) }

	cases := []struct {
		name string
		in   s3Checksums
		want string // Algorithm；空表示期望 nil
	}{
		{"none", s3Checksums{}, ""},
		// 新算法
		{"md5 only", s3Checksums{md5: p("m")}, "MD5"},
		{"sha512 only", s3Checksums{sha512: p("s512")}, "SHA512"},
		{"xxhash3 only", s3Checksums{xxh3: p("x3")}, "XXHASH3"},
		{"xxhash64 only", s3Checksums{xxh64: p("x64")}, "XXHASH64"},
		{"xxhash128 only", s3Checksums{xxh128: p("x128")}, "XXHASH128"},
		// 既有算法
		{"sha256 only", s3Checksums{sha256: p("s256")}, "SHA256"},
		{"sha1 only", s3Checksums{sha1: p("s1")}, "SHA1"},
		{"crc64 only", s3Checksums{crc64: p("c64")}, "CRC64NVME"},
		{"crc32c only", s3Checksums{crc32c: p("c32c")}, "CRC32C"},
		{"crc32 only", s3Checksums{crc32: p("c32")}, "CRC32"},
		// 优先级：SHA512 > SHA256 > SHA1 > MD5 > CRC64NVME > CRC32C > CRC32 > XXHASH128 > XXHASH64 > XXHASH3
		{"sha512 beats all", s3Checksums{sha512: p("a"), sha256: p("b"), md5: p("c"), crc32: p("d")}, "SHA512"},
		{"sha256 beats rest", s3Checksums{sha256: p("b"), sha1: p("c"), md5: p("d"), crc32: p("e")}, "SHA256"},
		{"md5 beats crc64", s3Checksums{md5: p("d"), crc64: p("e")}, "MD5"},
		{"crc64 beats xxhash", s3Checksums{crc64: p("e"), xxh128: p("f")}, "CRC64NVME"},
		{"xxh128 beats xxh64", s3Checksums{xxh128: p("f"), xxh64: p("g")}, "XXHASH128"},
		// 空串视为未返回
		{"empty sha256 falls through to crc32", s3Checksums{sha256: p(""), crc32: p("c32")}, "CRC32"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := checksumFrom(tc.in)
			if tc.want == "" {
				if got != nil {
					t.Fatalf("checksumFrom() = %+v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Fatalf("checksumFrom() = nil, want Algorithm=%q", tc.want)
			}
			if got.Algorithm != tc.want {
				t.Fatalf("Algorithm = %q, want %q", got.Algorithm, tc.want)
			}
		})
	}
}
