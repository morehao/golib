package storage

import (
	"testing"
)

func TestS3PathBuilder_Build(t *testing.T) {
	pb := &S3PathBuilder{
		BaseURL:  "https://cdn.example.com",
		Endpoint: "https://s3.example.com",
		URLStyle: URLStylePath,
	}
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
	if got, want := p.PublicURL(), "https://cdn.example.com/avatars/user/1.png"; got != want {
		t.Errorf("PublicURL = %q, want %q", got, want)
	}
}

func TestS3PathBuilder_PublicURLFallback(t *testing.T) {
	pb := &S3PathBuilder{
		BaseURL:  "",
		Endpoint: "https://s3.example.com",
		URLStyle: URLStylePath,
	}
	p := pb.Build("b", "k")
	if got, want := p.PublicURL(), "https://s3.example.com/b/k"; got != want {
		t.Errorf("PublicURL = %q, want %q", got, want)
	}
}

func TestS3PathBuilder_PublicURLBothEmpty(t *testing.T) {
	pb := &S3PathBuilder{
		BaseURL:  "",
		Endpoint: "",
		URLStyle: URLStylePath,
	}
	p := pb.Build("b", "k")
	if p.PublicURL() != "" {
		t.Errorf("PublicURL = %q, want empty", p.PublicURL())
	}
}

func TestS3PathBuilder_BuildCOSFormat(t *testing.T) {
	pb := &S3PathBuilder{
		BaseURL:  "https://cdn.example.com",
		Endpoint: "https://cos.ap-guangzhou.myqcloud.com",
		Region:   "ap-guangzhou",
		URLStyle: URLStyleVirtualHosted,
	}
	p := pb.Build("mybucket-1250000000", "user/1.png")
	if got, want := p.PublicURL(), "https://cdn.example.com/user/1.png"; got != want {
		t.Errorf("PublicURL = %q, want %q (COS BaseURL prefix)", got, want)
	}
}

func TestS3PathBuilder_COSFormatFallbackWithRegion(t *testing.T) {
	pb := &S3PathBuilder{
		BaseURL:  "",
		Endpoint: "https://cos.ap-guangzhou.myqcloud.com",
		Region:   "ap-guangzhou",
		URLStyle: URLStyleVirtualHosted,
	}
	p := pb.Build("mybucket-1250000000", "user/1.png")
	if got, want := p.PublicURL(), "https://mybucket-1250000000.cos.ap-guangzhou.myqcloud.com/user/1.png"; got != want {
		t.Errorf("PublicURL = %q, want %q (COS virtual hosted with region)", got, want)
	}
}

func TestS3PathBuilder_COSFormatFallbackNoRegion(t *testing.T) {
	pb := &S3PathBuilder{
		BaseURL:  "",
		Endpoint: "https://cos.ap-guangzhou.myqcloud.com",
		Region:   "",
		URLStyle: URLStyleVirtualHosted,
	}
	p := pb.Build("mybucket-1250000000", "user/1.png")
	if p.PublicURL() != "" {
		t.Errorf("PublicURL = %q, want empty when no region and no BaseURL", p.PublicURL())
	}
}

func TestS3PathBuilder_COSFormatBaseURLHasBucket(t *testing.T) {
	pb := &S3PathBuilder{
		BaseURL:  "https://mybucket-1250000000.cos.ap-guangzhou.myqcloud.com",
		Endpoint: "https://cos.ap-guangzhou.myqcloud.com",
		URLStyle: URLStyleVirtualHosted,
	}
	p := pb.Build("mybucket-1250000000", "user/1.png")
	if got, want := p.PublicURL(), "https://mybucket-1250000000.cos.ap-guangzhou.myqcloud.com/user/1.png"; got != want {
		t.Errorf("PublicURL = %q, want %q (COS custom BaseURL)", got, want)
	}
}

func TestLocalPathBuilder_Build(t *testing.T) {
	pb := &LocalPathBuilder{
		AbsDir:  "/data/storage",
		BaseURL: "http://localhost:8080",
	}
	p := pb.Build("avatars", "user/1.png")
	if p.IsLocal() != true {
		t.Error("IsLocal should be true")
	}
	if got, want := p.URI(), "file:///avatars/user/1.png"; got != want {
		t.Errorf("URI = %q, want %q", got, want)
	}
	if got, want := p.PublicURL(), "http://localhost:8080/avatars/user/1.png"; got != want {
		t.Errorf("PublicURL = %q, want %q", got, want)
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

func TestLocalPathBuilder_BuildNoHTTPBase(t *testing.T) {
	pb := &LocalPathBuilder{
		AbsDir:  "/data/storage",
		BaseURL: "",
	}
	p := pb.Build("avatars", "1.png")
	if got, want := p.PublicURL(), "file:///data/storage/data/avatars/1.png"; got != want {
		t.Errorf("PublicURL = %q, want %q", got, want)
	}
}

func TestLocalPathBuilder_AbsPathIncludesData(t *testing.T) {
	pb := &LocalPathBuilder{
		AbsDir:  "/var/storage",
		BaseURL: "",
	}
	p := pb.Build("mybucket", "dir/file.txt")
	pub := p.PublicURL()
	if want := "file:///var/storage/data/mybucket/dir/file.txt"; pub != want {
		t.Errorf("PublicURL = %q, want %q (must include /data/ for direct file access)", pub, want)
	}
}

func TestS3PathBuilder_ParsePublicURL_PathStyle(t *testing.T) {
	pb := &S3PathBuilder{
		BaseURL:  "https://cdn.example.com",
		Endpoint: "https://s3.example.com",
		URLStyle: URLStylePath,
	}
	p, err := pb.ParsePublicURL("https://cdn.example.com/avatars/user/1.png")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := p.Bucket(), "avatars"; got != want {
		t.Errorf("Bucket = %q, want %q", got, want)
	}
	if got, want := p.Key(), "user/1.png"; got != want {
		t.Errorf("Key = %q, want %q", got, want)
	}
	if got, want := p.URI(), "s3://avatars/user/1.png"; got != want {
		t.Errorf("URI = %q, want %q", got, want)
	}
}

func TestS3PathBuilder_ParsePublicURL_PathStyleFallbackEndpoint(t *testing.T) {
	pb := &S3PathBuilder{
		BaseURL:  "",
		Endpoint: "https://s3.example.com",
		URLStyle: URLStylePath,
	}
	p, err := pb.ParsePublicURL("https://s3.example.com/b/k")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := p.Bucket(), "b"; got != want {
		t.Errorf("Bucket = %q, want %q", got, want)
	}
	if got, want := p.Key(), "k"; got != want {
		t.Errorf("Key = %q, want %q", got, want)
	}
}

func TestS3PathBuilder_ParsePublicURL_VirtualHostedBaseURL(t *testing.T) {
	pb := &S3PathBuilder{
		BaseURL:  "https://cdn.example.com",
		Endpoint: "https://cos.ap-guangzhou.myqcloud.com",
		Region:   "ap-guangzhou",
		URLStyle: URLStyleVirtualHosted,
	}
	// 不传 WithBucket，兜底走 hostname
	p, err := pb.ParsePublicURL("https://cdn.example.com/user/1.png")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := p.Bucket(), "cdn.example.com"; got != want {
		t.Errorf("Bucket without WithBucket = %q, want %q", got, want)
	}
	if got, want := p.Key(), "user/1.png"; got != want {
		t.Errorf("Key = %q, want %q", got, want)
	}

	// 传 WithBucket 指定真实 bucket
	p2, err := pb.ParsePublicURL("https://cdn.example.com/user/1.png",
		WithBucket("mybucket-1250000000"))
	if err != nil {
		t.Fatalf("unexpected error with WithBucket: %v", err)
	}
	if got, want := p2.Bucket(), "mybucket-1250000000"; got != want {
		t.Errorf("Bucket with WithBucket = %q, want %q", got, want)
	}
}

func TestS3PathBuilder_ParsePublicURL_VirtualHostedBaseURLHasBucket(t *testing.T) {
	pb := &S3PathBuilder{
		BaseURL:  "https://mybucket-1250000000.cos.ap-guangzhou.myqcloud.com",
		Endpoint: "https://cos.ap-guangzhou.myqcloud.com",
		Region:   "ap-guangzhou",
		URLStyle: URLStyleVirtualHosted,
	}
	p, err := pb.ParsePublicURL("https://mybucket-1250000000.cos.ap-guangzhou.myqcloud.com/user/1.png")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := p.Bucket(), "mybucket-1250000000"; got != want {
		t.Errorf("Bucket = %q, want %q", got, want)
	}
	if got, want := p.Key(), "user/1.png"; got != want {
		t.Errorf("Key = %q, want %q", got, want)
	}
	if got, want := p.URI(), "s3://mybucket-1250000000/user/1.png"; got != want {
		t.Errorf("URI = %q, want %q", got, want)
	}
}

func TestS3PathBuilder_ParsePublicURL_VirtualHostedRegionFallback(t *testing.T) {
	pb := &S3PathBuilder{
		BaseURL:  "",
		Endpoint: "https://cos.ap-guangzhou.myqcloud.com",
		Region:   "ap-guangzhou",
		URLStyle: URLStyleVirtualHosted,
	}
	p, err := pb.ParsePublicURL("https://mybucket-1250000000.cos.ap-guangzhou.myqcloud.com/user/1.png")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := p.Bucket(), "mybucket-1250000000"; got != want {
		t.Errorf("Bucket = %q, want %q", got, want)
	}
	if got, want := p.Key(), "user/1.png"; got != want {
		t.Errorf("Key = %q, want %q", got, want)
	}
}

func TestS3PathBuilder_ParsePublicURL_NestedKey(t *testing.T) {
	pb := &S3PathBuilder{
		BaseURL:  "https://cdn.example.com",
		Endpoint: "https://s3.example.com",
		URLStyle: URLStylePath,
	}
	p, err := pb.ParsePublicURL("https://cdn.example.com/b/a/b/c/d.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := p.Key(), "a/b/c/d.txt"; got != want {
		t.Errorf("Key = %q, want %q", got, want)
	}
}

func TestS3PathBuilder_ParsePublicURL_NoKey(t *testing.T) {
	pb := &S3PathBuilder{
		BaseURL:  "https://cdn.example.com",
		Endpoint: "https://s3.example.com",
		URLStyle: URLStylePath,
	}
	p, err := pb.ParsePublicURL("https://cdn.example.com/mybucket")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := p.Bucket(), "mybucket"; got != want {
		t.Errorf("Bucket = %q, want %q", got, want)
	}
	if p.Key() != "" {
		t.Errorf("Key = %q, want empty", p.Key())
	}
}

func TestS3PathBuilder_ParsePublicURL_UnrecognizedPrefix(t *testing.T) {
	pb := &S3PathBuilder{
		BaseURL:  "https://cdn.example.com",
		Endpoint: "https://s3.example.com",
		URLStyle: URLStylePath,
	}
	_, err := pb.ParsePublicURL("https://other.example.com/bucket/key")
	if err == nil {
		t.Error("expected error for unrecognized URL prefix")
	}
}

func TestS3PathBuilder_ParsePublicURL_InvalidURL(t *testing.T) {
	pb := &S3PathBuilder{
		BaseURL:  "https://cdn.example.com",
		Endpoint: "https://s3.example.com",
		URLStyle: URLStylePath,
	}
	_, err := pb.ParsePublicURL("://invalid")
	if err == nil {
		t.Error("expected error for invalid URL")
	}
}

func TestLocalPathBuilder_ParsePublicURL(t *testing.T) {
	pb := &LocalPathBuilder{
		AbsDir:  "/data/storage",
		BaseURL: "http://localhost:8080",
	}
	p, err := pb.ParsePublicURL("http://localhost:8080/avatars/user/1.png")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := p.Bucket(), "avatars"; got != want {
		t.Errorf("Bucket = %q, want %q", got, want)
	}
	if got, want := p.Key(), "user/1.png"; got != want {
		t.Errorf("Key = %q, want %q", got, want)
	}
	if got, want := p.URI(), "file:///avatars/user/1.png"; got != want {
		t.Errorf("URI = %q, want %q", got, want)
	}
}

func TestLocalPathBuilder_ParsePublicURL_NoBaseURL(t *testing.T) {
	pb := &LocalPathBuilder{
		AbsDir:  "/data/storage",
		BaseURL: "",
	}
	_, err := pb.ParsePublicURL("http://localhost:8080/avatars/user/1.png")
	if err == nil {
		t.Error("expected error when BaseURL is empty")
	}
}

func TestLocalPathBuilder_ParsePublicURL_UnrecognizedPrefix(t *testing.T) {
	pb := &LocalPathBuilder{
		AbsDir:  "/data/storage",
		BaseURL: "http://localhost:8080",
	}
	_, err := pb.ParsePublicURL("http://other:9090/bucket/key")
	if err == nil {
		t.Error("expected error for unrecognized URL prefix")
	}
}

func TestS3PathBuilder_ParsePublicURL_COSCDN_WithBucket(t *testing.T) {
	pb := &S3PathBuilder{
		BaseURL:  "https://cdn.mydomain.com",
		Endpoint: "https://cos.ap-guangzhou.myqcloud.com",
		Region:   "ap-guangzhou",
		URLStyle: URLStyleVirtualHosted,
	}
	p, err := pb.ParsePublicURL("https://cdn.mydomain.com/user/1.png",
		WithBucket("mybucket-1250000000"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := p.Bucket(), "mybucket-1250000000"; got != want {
		t.Errorf("Bucket = %q, want %q", got, want)
	}
	if got, want := p.Key(), "user/1.png"; got != want {
		t.Errorf("Key = %q, want %q", got, want)
	}
	if got, want := p.URI(), "s3://mybucket-1250000000/user/1.png"; got != want {
		t.Errorf("URI = %q, want %q", got, want)
	}
}

func TestS3PathBuilder_ParsePublicURL_COSDefaultDomain_WithBucketRedundant(t *testing.T) {
	pb := &S3PathBuilder{
		BaseURL:  "https://mybucket-1250000000.cos.ap-guangzhou.myqcloud.com",
		Endpoint: "https://cos.ap-guangzhou.myqcloud.com",
		Region:   "ap-guangzhou",
		URLStyle: URLStyleVirtualHosted,
	}
	p, err := pb.ParsePublicURL("https://mybucket-1250000000.cos.ap-guangzhou.myqcloud.com/user/1.png",
		WithBucket("other-bucket"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := p.Bucket(), "mybucket-1250000000"; got != want {
		t.Errorf("Bucket = %q, want %q (domain should take priority over WithBucket)", got, want)
	}
}

func TestS3PathBuilder_ParsePublicURL_PathStyle_WithBucketIgnored(t *testing.T) {
	pb := &S3PathBuilder{
		BaseURL:  "https://cdn.example.com",
		Endpoint: "https://s3.example.com",
		URLStyle: URLStylePath,
	}
	p, err := pb.ParsePublicURL("https://cdn.example.com/avatars/user/1.png",
		WithBucket("ignored"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := p.Bucket(), "avatars"; got != want {
		t.Errorf("Bucket = %q, want %q (path-style ignores WithBucket)", got, want)
	}
}

func TestLocalPathBuilder_ParsePublicURL_WithBucketIgnored(t *testing.T) {
	pb := &LocalPathBuilder{
		AbsDir:  "/data/storage",
		BaseURL: "http://localhost:8080",
	}
	p, err := pb.ParsePublicURL("http://localhost:8080/avatars/user/1.png",
		WithBucket("ignored"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := p.Bucket(), "avatars"; got != want {
		t.Errorf("Bucket = %q, want %q (local ignores WithBucket)", got, want)
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
	pb := &LocalPathBuilder{AbsDir: "/data"}
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
