package storage

import (
	"testing"
)

func TestS3PathBuilder_Build(t *testing.T) {
	pb := &S3PathBuilder{}
	p := pb.Build("avatars", "user/1.png")
	if p.Bucket() != "avatars" {
		t.Errorf("Bucket = %q, want avatars", p.Bucket())
	}
	if p.Key() != "user/1.png" {
		t.Errorf("Key = %q, want user/1.png", p.Key())
	}
	if got, want := p.URI(), "s3://avatars/user/1.png"; got != want {
		t.Errorf("URI = %q, want %q", got, want)
	}
}

func TestLocalPathBuilder_Build(t *testing.T) {
	pb := &LocalPathBuilder{}
	p := pb.Build("avatars", "user/1.png")
	if p.IsLocal() != true {
		t.Error("IsLocal should be true")
	}
	if got, want := p.URI(), "file:///avatars/user/1.png"; got != want {
		t.Errorf("URI = %q, want %q", got, want)
	}
}

func TestParseURI_S3(t *testing.T) {
	scheme, bucket, key, err := ParseURI("s3://mybucket/user/1.png")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if scheme != "s3" {
		t.Errorf("scheme = %q, want s3", scheme)
	}
	if bucket != "mybucket" {
		t.Errorf("bucket = %q, want mybucket", bucket)
	}
	if key != "user/1.png" {
		t.Errorf("key = %q, want user/1.png", key)
	}
}

func TestParseURI_File(t *testing.T) {
	scheme, bucket, key, err := ParseURI("file:///avatars/data.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if scheme != "file" {
		t.Errorf("scheme = %q, want file", scheme)
	}
	if bucket != "avatars" {
		t.Errorf("bucket = %q, want avatars", bucket)
	}
	if key != "data.json" {
		t.Errorf("key = %q, want data.json", key)
	}
}

func TestParseURI_NestedKey(t *testing.T) {
	_, bucket, key, err := ParseURI("s3://b/a/b/c/d.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if bucket != "b" {
		t.Errorf("bucket = %q, want b", bucket)
	}
	if key != "a/b/c/d.txt" {
		t.Errorf("key = %q, want a/b/c/d.txt", key)
	}
}

func TestParseURI_EncodedKey(t *testing.T) {
	_, _, key, err := ParseURI("s3://b/a%20b.png")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key != "a b.png" {
		t.Errorf("key = %q, want a b.png (decoded)", key)
	}
}

func TestParseURI_EmptyKey(t *testing.T) {
	_, _, key, err := ParseURI("s3://mybucket/")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key != "" {
		t.Errorf("key = %q, want empty", key)
	}
}

func TestParseURI_Invalid(t *testing.T) {
	tests := []string{
		"",
		"no-scheme",
		"://nobucket",
		"s3://",
		"ftp://bucket/key",
	}
	for _, uri := range tests {
		_, _, _, err := ParseURI(uri)
		if err == nil {
			t.Errorf("ParseURI(%q) should return error", uri)
		}
	}
}

func TestParseURI_NoKey(t *testing.T) {
	scheme, bucket, key, err := ParseURI("s3://mybucket")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if scheme != "s3" || bucket != "mybucket" || key != "" {
		t.Errorf("got (%q, %q, %q), want (s3, mybucket, \"\")", scheme, bucket, key)
	}
}

func TestURIAndParseURI_RoundTrip_S3(t *testing.T) {
	tests := []struct {
		bucket string
		key    string
	}{
		{"mybucket", "user/1.png"},
		{"b", "a/b/c/d.txt"},
		{"mybucket", ""},
		{"mybucket", "文件 名称.txt"},
	}
	pb := &S3PathBuilder{}
	for _, tt := range tests {
		p := pb.Build(tt.bucket, tt.key)
		uri := p.URI()
		scheme, bucket, key, err := ParseURI(uri)
		if err != nil {
			t.Errorf("ParseURI(%q) error: %v", uri, err)
			continue
		}
		if scheme != SchemeS3 {
			t.Errorf("scheme = %q, want %q", scheme, SchemeS3)
		}
		if bucket != tt.bucket {
			t.Errorf("bucket = %q, want %q", bucket, tt.bucket)
		}
		if key != tt.key {
			t.Errorf("key = %q, want %q", key, tt.key)
		}
	}
}

func TestURIAndParseURI_RoundTrip_File(t *testing.T) {
	tests := []struct {
		bucket string
		key    string
	}{
		{"avatars", "user/1.png"},
		{"data", "a/b/c/d.txt"},
		{"root", ""},
		{"root", "文件 名称.txt"},
	}
	pb := &LocalPathBuilder{}
	for _, tt := range tests {
		p := pb.Build(tt.bucket, tt.key)
		uri := p.URI()
		scheme, bucket, key, err := ParseURI(uri)
		if err != nil {
			t.Errorf("ParseURI(%q) error: %v", uri, err)
			continue
		}
		if scheme != SchemeFile {
			t.Errorf("scheme = %q, want %q", scheme, SchemeFile)
		}
		if bucket != tt.bucket {
			t.Errorf("bucket = %q, want %q", bucket, tt.bucket)
		}
		if key != tt.key {
			t.Errorf("key = %q, want %q", key, tt.key)
		}
	}
}

func TestBuildURI_S3(t *testing.T) {
	tests := []struct {
		bucket string
		key    string
		want   string
	}{
		{"mybucket", "user/1.png", "s3://mybucket/user/1.png"},
		{"b", "a/b/c/d.txt", "s3://b/a/b/c/d.txt"},
		{"mybucket", "", "s3://mybucket"},
	}
	for _, tt := range tests {
		got, err := BuildURI(SchemeS3, tt.bucket, tt.key)
		if err != nil {
			t.Errorf("BuildURI(s3, %q, %q) unexpected error: %v", tt.bucket, tt.key, err)
			continue
		}
		if got != tt.want {
			t.Errorf("BuildURI(s3, %q, %q) = %q, want %q", tt.bucket, tt.key, got, tt.want)
		}
	}
}

func TestBuildURI_File(t *testing.T) {
	tests := []struct {
		bucket string
		key    string
		want   string
	}{
		{"avatars", "user/1.png", "file:///avatars/user/1.png"},
		{"data", "a/b/c/d.txt", "file:///data/a/b/c/d.txt"},
		{"root", "", "file:///root"},
	}
	for _, tt := range tests {
		got, err := BuildURI(SchemeFile, tt.bucket, tt.key)
		if err != nil {
			t.Errorf("BuildURI(file, %q, %q) unexpected error: %v", tt.bucket, tt.key, err)
			continue
		}
		if got != tt.want {
			t.Errorf("BuildURI(file, %q, %q) = %q, want %q", tt.bucket, tt.key, got, tt.want)
		}
	}
}

func TestBuildURI_Invalid(t *testing.T) {
	tests := []struct {
		scheme string
		bucket string
		key    string
	}{
		{"ftp", "b", "k"},
		{"", "b", "k"},
		{SchemeS3, "", "k"},
	}
	for _, tt := range tests {
		_, err := BuildURI(tt.scheme, tt.bucket, tt.key)
		if err == nil {
			t.Errorf("BuildURI(%q, %q, %q) should return error", tt.scheme, tt.bucket, tt.key)
		}
	}
}

func TestBuildURIAndParseURI_RoundTrip(t *testing.T) {
	cases := []struct {
		scheme string
		bucket string
		key    string
	}{
		{SchemeS3, "mybucket", "user/1.png"},
		{SchemeS3, "b", "a/b/c/d.txt"},
		{SchemeS3, "mybucket", ""},
		{SchemeFile, "avatars", "user/1.png"},
		{SchemeFile, "root", ""},
		{SchemeFile, "data", "a b/中文.txt"},
	}
	for _, c := range cases {
		uri, err := BuildURI(c.scheme, c.bucket, c.key)
		if err != nil {
			t.Errorf("BuildURI(%q, %q, %q) unexpected error: %v", c.scheme, c.bucket, c.key, err)
			continue
		}
		scheme, bucket, key, err := ParseURI(uri)
		if err != nil {
			t.Errorf("ParseURI(%q) unexpected error: %v", uri, err)
			continue
		}
		if scheme != c.scheme || bucket != c.bucket || key != c.key {
			t.Errorf("round-trip mismatch: BuildURI(%q,%q,%q)=%q, ParseURI=%q/%q/%q",
				c.scheme, c.bucket, c.key, uri, scheme, bucket, key)
		}
	}
}
