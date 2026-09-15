// Package s3base 基于 aws-sdk-go-v2/service/s3 的统一 S3 driver 实现，
// 供 minio / oss / tos / cos 等 S3 兼容后端共用。
package s3base

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/morehao/golib/storage"
	"github.com/morehao/golib/storage/driver/internal/pathcheck"
)

const (
	// defaultDriverName 在 profile 未指定 Name 时使用的错误上下文名称。
	defaultDriverName = "s3"
)

// Driver 基于 aws-sdk-go-v2/service/s3 的统一 S3 驱动实现。
type Driver struct {
	client           *s3.Client
	presign          *s3.PresignClient
	region           string
	name             string
	caps             storage.Caps
	profile          ProviderProfile
	pb               storage.PathBuilder
	ifNotExistsS3Opt func(*s3.Options)
}

var _ storage.Storage = (*Driver)(nil)

// Option 为 s3base.New 提供可选配置。
type Option func(*options)

type options struct {
	profile *ProviderProfile
	s3Opts  []func(*s3.Options)
}

// WithProfile 注入供应商差异表。这是 s3base 认识具体供应商的唯一入口：
// 共享基类里不允许再出现任何厂商名或厂商分支。
func WithProfile(p ProviderProfile) Option {
	return func(o *options) {
		o.profile = &p
	}
}

// WithS3Options 传入 aws 原生 S3 客户端选项，供测试注入或一次性调优使用。
// 供应商差异应写进 ProviderProfile，而不是走这个口子。
func WithS3Options(s3Opts ...func(*s3.Options)) Option {
	return func(o *options) {
		o.s3Opts = append(o.s3Opts, s3Opts...)
	}
}

// normalizeEndpoint 补全 endpoint 的 scheme。
//
// aws-sdk-go-v2 要求 BaseEndpoint 是完整 URI；只给 "host:port" 会在签名阶段报
// "resolve endpoint: Custom endpoint ... was not a valid URI"。
// useSSL 仅在 endpoint 未带 scheme 时用于选择 http/https。
func normalizeEndpoint(endpoint string, useSSL bool) (string, error) {
	if endpoint == "" {
		return "", fmt.Errorf("%w: Endpoint is required", storage.ErrInvalidConfig)
	}
	if strings.Contains(endpoint, "://") {
		u, err := url.Parse(endpoint)
		if err != nil {
			return "", fmt.Errorf("%w: invalid Endpoint %q: %v", storage.ErrInvalidConfig, endpoint, err)
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return "", fmt.Errorf("%w: Endpoint scheme %q must be http or https", storage.ErrInvalidConfig, u.Scheme)
		}
		return endpoint, nil
	}
	scheme := "http"
	if useSSL {
		scheme = "https"
	}
	return scheme + "://" + endpoint, nil
}

func New(cfg storage.Config, pb storage.PathBuilder, opts ...Option) (storage.Storage, error) {
	o := &options{}
	for _, opt := range opts {
		opt(o)
	}
	if o.profile == nil {
		return nil, fmt.Errorf("%w: ProviderProfile is required", storage.ErrInvalidConfig)
	}
	profile := *o.profile
	if profile.Name == "" {
		profile.Name = defaultDriverName
	}
	// VendorHeader 模式必须真的带上那个头，否则条件写会静默退化为覆盖写。
	if profile.ConditionalWrite == storage.ConditionalWriteVendorHeader && profile.ConditionalWriteOption == nil {
		return nil, fmt.Errorf("%w: ConditionalWrite=VendorHeader requires ConditionalWriteOption", storage.ErrInvalidConfig)
	}
	if cfg.AccessKey == "" {
		return nil, fmt.Errorf("%w: AccessKey is required", storage.ErrInvalidConfig)
	}
	if cfg.Region == "" {
		return nil, fmt.Errorf("%w: Region is required", storage.ErrInvalidConfig)
	}
	endpoint, err := normalizeEndpoint(cfg.Endpoint, cfg.UseSSL)
	if err != nil {
		return nil, err
	}
	loadOpts := []func(*config.LoadOptions) error{
		config.WithRegion(cfg.Region),
		config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, ""),
		),
	}
	if cfg.Retry.MaxAttempts > 0 {
		// ADR-6：让重试次数成为真实生效的配置项，而不是一个被静默忽略的字段。
		// SDK v2 的 MaxAttempts 是"尝试次数"（v1 的 maxRetries 是"重试次数"），
		// 语义已在 storage.RetryConfig 上写明。
		loadOpts = append(loadOpts, config.WithRetryMaxAttempts(cfg.Retry.MaxAttempts))
	}
	awsCfg, err := config.LoadDefaultConfig(context.Background(), loadOpts...)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", storage.ErrInvalidConfig, err)
	}
	client := s3.NewFromConfig(awsCfg, func(c *s3.Options) {
		c.BaseEndpoint = aws.String(endpoint)
		// 寻址风格来自 profile 数据，不再靠嗅探 endpoint 里的厂商域名。
		c.UsePathStyle = profile.ForcePathStyle
		for _, apiOpt := range profile.APIOptions {
			c.APIOptions = append(c.APIOptions, apiOpt)
		}
		for _, s3OptFn := range o.s3Opts {
			s3OptFn(c)
		}
	})
	d := &Driver{
		client:  client,
		presign: s3.NewPresignClient(client),
		region:  cfg.Region,
		name:    profile.Name,
		caps:    profile.caps(),
		profile: profile,
		pb:      pb,
	}
	if profile.ConditionalWrite == storage.ConditionalWriteVendorHeader {
		d.ifNotExistsS3Opt = profile.ConditionalWriteOption
	}
	return d, nil
}

// Caps 声明本 driver 的能力与协议硬限制，数据来自 ProviderProfile。
func (d *Driver) Caps() storage.Caps {
	return d.caps
}

// PathBuilder 返回当前 driver 使用的 PathBuilder，调用方可用于 Build。
func (d *Driver) PathBuilder() storage.PathBuilder {
	return d.pb
}

func (d *Driver) newPath(bucket, key string) storage.StoragePath {
	return d.pb.Build(bucket, key)
}

// newPath 供 PathBuilder 的调用方渲染 URI；ObjectInfo 不再承载 StoragePath，
// 那属于"展示/持久化"层，不应混进协议原生元数据。

// wrapErr 把 SDK 错误转成带协议上下文的 storage.OpError。
//
// 保留 Op/Code/Status/RequestID 是为了排障（提工单需要 request id）；
// 分类走 s3ErrorKind 表，使调用方能用 errors.Is(err, storage.ErrNotFound)
// 这类与后端无关的判断。
func (d *Driver) wrapErr(op, bucket, key string, err error) error {
	if err == nil {
		return nil
	}
	// 上下文取消/超时是调用方主动放弃，不是后端故障：原样返回，
	// 避免被上层当成 S3 错误记录或重试。
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	// 已分类过的错误不再包装（例如 pathcheck 返回的 sentinel）。
	var existing *storage.OpError
	if errors.As(err, &existing) {
		return err
	}
	f := inspectS3Err(err, d.profile.ErrorCodeKind)
	if f.op != "" {
		op = f.op
	}
	return &storage.OpError{
		Driver:    d.name,
		Op:        op,
		Bucket:    bucket,
		Key:       key,
		Kind:      f.kind,
		Code:      f.code,
		Status:    f.status,
		RequestID: f.requestID,
		Err:       err,
	}
}

// ---------- 请求构造（抽为纯函数，便于零网络单测参数构造） ----------

func putObjectInput(bucket, key string, o *storage.PutOptions) *s3.PutObjectInput {
	input := &s3.PutObjectInput{
		Bucket:       aws.String(bucket),
		Key:          aws.String(key),
		ContentType:  strPtr(o.ContentType),
		ContentMD5:   strPtr(o.ContentMD5),
		Metadata:     o.Metadata,
		StorageClass: types.StorageClass(o.StorageClass),
	}
	if o.IfNotExists {
		// 条件写：S3 原生的 If-None-Match:* 由后端原子保证。
		// 不支持该语义的后端必须显式降级，不能静默退化成覆盖写。
		input.IfNoneMatch = aws.String("*")
	}
	return input
}

func copyObjectInput(srcBucket, srcKey, dstBucket, dstKey string) *s3.CopyObjectInput {
	return &s3.CopyObjectInput{
		Bucket:     aws.String(dstBucket),
		Key:        aws.String(dstKey),
		CopySource: aws.String(copySourceValue(srcBucket, srcKey)),
	}
}

func createMultipartUploadInput(bucket, key string, in storage.CreateMultipartInput) *s3.CreateMultipartUploadInput {
	return &s3.CreateMultipartUploadInput{
		Bucket:       aws.String(bucket),
		Key:          aws.String(key),
		ContentType:  strPtr(in.ContentType),
		Metadata:     in.Metadata,
		StorageClass: types.StorageClass(in.StorageClass),
	}
}

// listObjectsInput 构造 ListObjectsV2Input。
//
// StartAfter 与 ContinuationToken 互斥：同时下发时 S3 语义未定义，
// 因此有 token 时只发 token。maxPage 取自 Caps 声明的后端页大小上限。
func listObjectsInput(bucket, prefix string, o *storage.ListOptions, maxPage int32) *s3.ListObjectsV2Input {
	input := &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket),
		Prefix: aws.String(prefix),
	}
	if o.ContinuationToken != "" {
		input.ContinuationToken = aws.String(o.ContinuationToken)
	} else {
		input.StartAfter = strPtr(o.StartAfter)
	}
	if n := clampMaxKeys(o.MaxKeys, maxPage); n > 0 {
		input.MaxKeys = aws.Int32(n)
	}
	if !o.Recursive {
		input.Delimiter = aws.String("/")
	}
	return input
}

// 分批逻辑统一由共享层 storage.DeleteObjectsChunked 提供（含零网络单测），
// 不再在本驱动内各写一份：旧实现完全不分批，超过 1000 个 key 时
// 整个 DeleteObjects 请求会被后端拒绝。

// clampMaxKeys 把 MaxKeys 收敛到后端允许的范围；<=0 表示不下发该字段（用后端默认值）。
// 旧实现直接 int32(o.MaxKeys)，int64 超出 int32 范围时溢出成负数并导致请求被拒。
func clampMaxKeys(n int64, maxPage int32) int32 {
	if n <= 0 {
		return 0
	}
	if maxPage <= 0 {
		// 后端未声明页大小上限：仍需防溢出，超出 int32 时退化为"不下发"。
		if n > math.MaxInt32 {
			return 0
		}
		return int32(n)
	}
	if n > int64(maxPage) {
		return maxPage
	}
	return int32(n)
}

// ---------- 字节计数 ----------

// countingReader 统计单次顺序读取的字节数。
type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

// countingReadSeeker 统计"到达过的最大绝对偏移"，作为对象大小。
// 用最大偏移而不是累加，是为了在 SDK 重试回绕 body 后重复读取时不重复计数。
type countingReadSeeker struct {
	rs  io.ReadSeeker
	pos int64
	max int64
}

func (c *countingReadSeeker) Read(p []byte) (int, error) {
	n, err := c.rs.Read(p)
	c.pos += int64(n)
	if c.pos > c.max {
		c.max = c.pos
	}
	return n, err
}

func (c *countingReadSeeker) Seek(offset int64, whence int) (int64, error) {
	pos, err := c.rs.Seek(offset, whence)
	if err != nil {
		return pos, err
	}
	c.pos = pos
	return pos, nil
}

// newCountingBody 包装上传 body，返回实际写入字节数的读取函数。
//
// 为什么必须自己计数：PutObjectOutput.Size 只在向 S3 Express One Zone 目录桶
// 追加（append）的场景返回，普通 PutObject 恒为 nil，aws.ToInt64 得到 0，
// 直接采信就会把对象大小落库为 0。
//
// 若 body 本身可 Seek（例如 *os.File、*bytes.Reader），包装后继续暴露 Seek，
// 保证 SDK 在重试时仍能回绕 body 重新发送。
func newCountingBody(body io.Reader) (io.Reader, func() int64) {
	if rs, ok := body.(io.ReadSeeker); ok {
		c := &countingReadSeeker{rs: rs}
		return c, func() int64 { return c.max }
	}
	c := &countingReader{r: body}
	return c, func() int64 { return c.n }
}

// ---------- Base ----------

func (d *Driver) PutObject(ctx context.Context, bucket, key string, body io.Reader, opts ...storage.PutOption) (*storage.PutObjectResult, error) {
	if err := pathcheck.ValidateBucket(bucket); err != nil {
		return nil, err
	}
	if err := pathcheck.ValidateKey(key); err != nil {
		return nil, err
	}
	o := &storage.PutOptions{}
	for _, opt := range opts {
		opt(o)
	}
	// 不支持条件写的后端必须显式拒绝，不得静默退化为覆盖写 —— 那会丢掉
	// 并发去重所依赖的原子性，且故障是"偶发覆盖"而非"报错"，极难排查。
	if o.IfNotExists && d.caps.ConditionalWrite == storage.ConditionalWriteNone {
		return nil, fmt.Errorf("%w: driver %s cannot guarantee conditional write", storage.ErrNotSupported, d.name)
	}
	countingBody, written := newCountingBody(body)
	input := putObjectInput(bucket, key, o)
	input.Body = countingBody

	putOpts := make([]func(*s3.Options), 0, 1)
	if o.IfNotExists && d.ifNotExistsS3Opt != nil {
		putOpts = append(putOpts, d.ifNotExistsS3Opt)
	}
	output, err := d.client.PutObject(ctx, input, putOpts...)
	if err != nil {
		return nil, d.wrapErr("PutObject", bucket, key, err)
	}
	etag := trimETag(aws.ToString(output.ETag))
	return &storage.PutObjectResult{
		ObjectInfo: storage.ObjectInfo{
			Bucket: bucket,
			Key:    key,
			// 大小为客户端计数值，见 newCountingBody 的说明。
			Size:        written(),
			ETag:        etag,
			ContentType: o.ContentType,
			// PutObject 响应不返回 LastModified。这里刻意留零值而不是用
			// 本机时间伪造：本机时钟带时区，与其它路径返回的服务端 UTC 时间
			// 不一致，伪造会掩盖"该字段未知"这一事实。需要时调用 HeadObject。
			Metadata:     o.Metadata,
			StorageClass: o.StorageClass,
			VersionID:    aws.ToString(output.VersionId),
		},
	}, nil
}

// parseContentRange 解析 S3 的 ContentRange 形如 "bytes 0-99/1234"。
// 整对象大小只能从这里拿：Range 请求下 ContentLength 是**段**长度，
// 而契约要求 ObjectInfo.Size 恒为整对象大小。
func parseContentRange(v string) (start, end, total int64, ok bool) {
	v = strings.TrimSpace(strings.TrimPrefix(v, "bytes"))
	v = strings.TrimSpace(v)
	rangePart, totalPart, found := strings.Cut(v, "/")
	if !found {
		return 0, 0, 0, false
	}
	startPart, endPart, found := strings.Cut(rangePart, "-")
	if !found {
		return 0, 0, 0, false
	}
	start, err1 := strconv.ParseInt(strings.TrimSpace(startPart), 10, 64)
	end, err2 := strconv.ParseInt(strings.TrimSpace(endPart), 10, 64)
	total, err3 := strconv.ParseInt(strings.TrimSpace(totalPart), 10, 64)
	if err1 != nil || err2 != nil || err3 != nil {
		return 0, 0, 0, false
	}
	return start, end, total, true
}

// archiveStorageClasses 是需要 restore 才能读取的存储类型。
var archiveStorageClasses = map[string]bool{
	"GLACIER":      true,
	"GLACIER_IR":   true,
	"DEEP_ARCHIVE": true,
	"ARCHIVE":      true, // 部分兼容后端（如 COS）的归档类型
	"COLD":         true,
}

// isArchived 由存储类型判断对象是否需要 restore 才能读。
// 这样做而不是等 GetObject 报 InvalidObjectState，是为了让上层能在
// **发起读取之前**就给出明确提示。
func isArchived(storageClass string) bool {
	return archiveStorageClasses[strings.ToUpper(storageClass)]
}

// checksumFrom 提取服务端返回的校验和；都没有时返回 nil（而不是空结构体，
// 以免上层以为"有校验和但值为空"）。
func checksumFrom(crc32, crc32c, crc64, sha1, sha256 *string) *storage.Checksum {
	switch {
	case aws.ToString(sha256) != "":
		return &storage.Checksum{Algorithm: "SHA256", Value: aws.ToString(sha256)}
	case aws.ToString(sha1) != "":
		return &storage.Checksum{Algorithm: "SHA1", Value: aws.ToString(sha1)}
	case aws.ToString(crc32) != "":
		return &storage.Checksum{Algorithm: "CRC32", Value: aws.ToString(crc32)}
	case aws.ToString(crc32c) != "":
		return &storage.Checksum{Algorithm: "CRC32C", Value: aws.ToString(crc32c)}
	case aws.ToString(crc64) != "":
		return &storage.Checksum{Algorithm: "CRC64NVME", Value: aws.ToString(crc64)}
	}
	return nil
}

func (d *Driver) GetObject(ctx context.Context, bucket, key string, opts ...storage.GetOption) (*storage.GetObjectResult, error) {
	if err := pathcheck.ValidateBucket(bucket); err != nil {
		return nil, err
	}
	if err := pathcheck.ValidateKey(key); err != nil {
		return nil, err
	}
	o := &storage.GetOptions{}
	for _, opt := range opts {
		opt(o)
	}
	input := &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	}
	if o.ByteRange != nil {
		input.Range = aws.String(fmt.Sprintf("bytes=%d-%d", o.ByteRange.Start, o.ByteRange.End))
	}
	output, err := d.client.GetObject(ctx, input)
	if err != nil {
		return nil, d.wrapErr("GetObject", bucket, key, err)
	}
	storageClass := string(output.StorageClass)
	info := storage.ObjectInfo{
		Bucket:       bucket,
		Key:          key,
		Size:         aws.ToInt64(output.ContentLength),
		ETag:         trimETag(aws.ToString(output.ETag)),
		ContentType:  aws.ToString(output.ContentType),
		LastModified: aws.ToTime(output.LastModified).UTC(),
		Metadata:     output.Metadata,
		StorageClass: storageClass,
		Archived:     isArchived(storageClass),
		VersionID:    aws.ToString(output.VersionId),
	}
	result := &storage.GetObjectResult{Body: output.Body, Info: info}
	if o.ByteRange != nil {
		// Range 请求下 ContentLength 是段长度，整对象大小只能来自 ContentRange。
		if start, end, total, ok := parseContentRange(aws.ToString(output.ContentRange)); ok {
			result.Range = &storage.RangeInfo{Start: start, End: end}
			result.Info.Size = total
		} else {
			// 后端没回 ContentRange：整对象大小不可知。如实置 -1 而不是
			// 把段长度冒充整对象大小（那正是修复前的行为）。
			result.Info.Size = -1
		}
	}
	return result, nil
}

func (d *Driver) DeleteObject(ctx context.Context, bucket, key string) error {
	if err := pathcheck.ValidateBucket(bucket); err != nil {
		return err
	}
	if err := pathcheck.ValidateKey(key); err != nil {
		return err
	}
	_, err := d.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	return d.wrapErr("DeleteObject", bucket, key, err)
}

func (d *Driver) DeleteObjects(ctx context.Context, bucket string, keys []string) error {
	if err := pathcheck.ValidateBucket(bucket); err != nil {
		return err
	}
	if len(keys) == 0 {
		return nil
	}
	for _, k := range keys {
		if err := pathcheck.ValidateKey(k); err != nil {
			return err
		}
	}
	// 分批逻辑由共享层提供（storage.DeleteObjectsChunked），保证与 local 等
	// 其它后端行为一致；批量上限取自 Caps 声明，避免"声明的限制"与
	// "实际分批大小"两处各写一遍而漂移。
	// Quiet=true 让后端只回错误项，省掉 1000 条冗余的成功回执。
	var failures []storage.DeleteFailure
	err := storage.DeleteObjectsChunked(keys, d.caps.Limits.MaxDeleteBatch, func(batch []string) error {
		objects := make([]types.ObjectIdentifier, len(batch))
		for i, k := range batch {
			objects[i] = types.ObjectIdentifier{Key: aws.String(k)}
		}
		output, err := d.client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
			Bucket: aws.String(bucket),
			Delete: &types.Delete{Objects: objects, Quiet: aws.Bool(true)},
		})
		if err != nil {
			return d.wrapErr("DeleteObjects", bucket, "", err)
		}
		for _, e := range output.Errors {
			failures = append(failures, storage.DeleteFailure{
				Key: aws.ToString(e.Key),
				Err: fmt.Errorf("%s: %s", aws.ToString(e.Code), aws.ToString(e.Message)),
			})
		}
		return nil
	})
	if err != nil {
		return err
	}
	if len(failures) > 0 {
		return &storage.BulkDeleteError{Failures: failures}
	}
	return nil
}

func (d *Driver) ListObjects(ctx context.Context, bucket, prefix string, opts ...storage.ListOption) (*storage.ListObjectsOutput, error) {
	if err := pathcheck.ValidateBucket(bucket); err != nil {
		return nil, err
	}
	o := &storage.ListOptions{}
	for _, opt := range opts {
		opt(o)
	}
	output, err := d.client.ListObjectsV2(ctx, listObjectsInput(bucket, prefix, o, d.caps.Limits.MaxListPage))
	if err != nil {
		return nil, d.wrapErr("ListObjectsV2", bucket, prefix, err)
	}
	contents := make([]storage.ObjectInfo, 0, len(output.Contents))
	for _, obj := range output.Contents {
		if obj.Key == nil || *obj.Key == "" {
			continue
		}
		storageClass := string(obj.StorageClass)
		contents = append(contents, storage.ObjectInfo{
			Bucket: bucket,
			Key:    aws.ToString(obj.Key),
			// 整对象大小由服务端给出：List 不返回内容，无段长度歧义。
			Size: aws.ToInt64(obj.Size),
			// ETag 必须与 Head/Get 走同一套规范化：S3 返回的 ETag 带引号，
			// 漏掉 trimETag 会让同一对象在 List 与 Head 处报出不同字符串。
			ETag:         trimETag(aws.ToString(obj.ETag)),
			LastModified: aws.ToTime(obj.LastModified).UTC(),
			StorageClass: storageClass,
			Archived:     isArchived(storageClass),
		})
	}
	common := make([]string, 0, len(output.CommonPrefixes))
	for _, p := range output.CommonPrefixes {
		common = append(common, aws.ToString(p.Prefix))
	}
	out := &storage.ListObjectsOutput{
		Contents:       contents,
		CommonPrefixes: common,
		IsTruncated:    aws.ToBool(output.IsTruncated),
	}
	if output.NextContinuationToken != nil {
		out.NextContinuationToken = *output.NextContinuationToken
	}
	return out, nil
}

// ---------- Multipart ----------

func (d *Driver) CreateMultipart(ctx context.Context, bucket, key string, in storage.CreateMultipartInput) (string, error) {
	if err := pathcheck.ValidateBucket(bucket); err != nil {
		return "", err
	}
	if err := pathcheck.ValidateKey(key); err != nil {
		return "", err
	}
	output, err := d.client.CreateMultipartUpload(ctx, createMultipartUploadInput(bucket, key, in))
	if err != nil {
		return "", d.wrapErr("CreateMultipartUpload", bucket, key, err)
	}
	return aws.ToString(output.UploadId), nil
}

func (d *Driver) UploadPart(ctx context.Context, ref storage.MultipartRef, number int32, body io.Reader) (*storage.PartInfo, error) {
	if err := pathcheck.ValidateBucket(ref.Bucket); err != nil {
		return nil, err
	}
	if err := pathcheck.ValidateKey(ref.Key); err != nil {
		return nil, err
	}
	if ref.UploadID == "" {
		return nil, fmt.Errorf("%w: upload_id is required", storage.ErrInvalidArgument)
	}
	if err := storage.ValidatePartCount(number, d.caps); err != nil {
		return nil, err
	}
	// S3 的 UploadPart 响应不含分片大小，与 PutObject 同理：由客户端计数得出。
	countingBody, written := newCountingBody(body)
	output, err := d.client.UploadPart(ctx, &s3.UploadPartInput{
		Bucket:     aws.String(ref.Bucket),
		Key:        aws.String(ref.Key),
		UploadId:   aws.String(ref.UploadID),
		PartNumber: aws.Int32(number),
		Body:       countingBody,
	})
	if err != nil {
		return nil, d.wrapErr("UploadPart", ref.Bucket, ref.Key, err)
	}
	return &storage.PartInfo{
		PartNumber: number,
		ETag:       trimETag(aws.ToString(output.ETag)),
		Size:       written(),
	}, nil
}

// ListParts 列出会话中已成功上传的分片：客户端崩溃后据此续传，
// 不必从零重传。分页语义与 S3 一致，IsTruncated 如实回报 ——
// 一次分片上传最多 10000 片而单页上限 1000，静默截断会让调用方
// 以为"只有 1000 片"。
func (d *Driver) ListParts(ctx context.Context, ref storage.MultipartRef, opts ...storage.ListPartsOption) (*storage.ListPartsOutput, error) {
	if err := pathcheck.ValidateBucket(ref.Bucket); err != nil {
		return nil, err
	}
	if err := pathcheck.ValidateKey(ref.Key); err != nil {
		return nil, err
	}
	if ref.UploadID == "" {
		return nil, fmt.Errorf("%w: upload_id is required", storage.ErrInvalidArgument)
	}
	o := &storage.ListPartsOptions{}
	for _, opt := range opts {
		opt(o)
	}
	input := &s3.ListPartsInput{
		Bucket:   aws.String(ref.Bucket),
		Key:      aws.String(ref.Key),
		UploadId: aws.String(ref.UploadID),
	}
	if o.MaxParts > 0 {
		input.MaxParts = aws.Int32(o.MaxParts)
	}
	if o.PartNumberMarker > 0 {
		input.PartNumberMarker = aws.String(strconv.FormatInt(int64(o.PartNumberMarker), 10))
	}
	output, err := d.client.ListParts(ctx, input)
	if err != nil {
		return nil, d.wrapErr("ListParts", ref.Bucket, ref.Key, err)
	}
	parts := make([]storage.PartInfo, 0, len(output.Parts))
	for _, p := range output.Parts {
		parts = append(parts, storage.PartInfo{
			PartNumber:   aws.ToInt32(p.PartNumber),
			ETag:         trimETag(aws.ToString(p.ETag)),
			Size:         aws.ToInt64(p.Size),
			LastModified: aws.ToTime(p.LastModified).UTC(),
		})
	}
	out := &storage.ListPartsOutput{
		Parts:       parts,
		IsTruncated: aws.ToBool(output.IsTruncated),
	}
	if n, err := strconv.ParseInt(aws.ToString(output.NextPartNumberMarker), 10, 32); err == nil {
		out.NextPartNumberMarker = int32(n)
	}
	return out, nil
}

func (d *Driver) CompleteMultipart(ctx context.Context, ref storage.MultipartRef, parts []storage.PartInfo) (*storage.ObjectInfo, error) {
	if err := pathcheck.ValidateBucket(ref.Bucket); err != nil {
		return nil, err
	}
	if err := pathcheck.ValidateKey(ref.Key); err != nil {
		return nil, err
	}
	if ref.UploadID == "" {
		return nil, fmt.Errorf("%w: upload_id is required", storage.ErrInvalidArgument)
	}
	if err := storage.ValidateParts(parts, d.caps); err != nil {
		return nil, err
	}
	cp := make([]types.CompletedPart, len(parts))
	var total int64
	sizesKnown := true
	for i, p := range parts {
		// S3 要求回传的 ETag 与 UploadPart 返回时形态一致（带引号）。
		// 契约里的 ETag 已规范化（无引号），因此出站前补回。
		cp[i] = types.CompletedPart{
			PartNumber: aws.Int32(p.PartNumber),
			ETag:       aws.String(quoteETag(p.ETag)),
		}
		if p.Size > 0 {
			total += p.Size
		} else {
			sizesKnown = false
		}
	}
	output, err := d.client.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket:          aws.String(ref.Bucket),
		Key:             aws.String(ref.Key),
		UploadId:        aws.String(ref.UploadID),
		MultipartUpload: &types.CompletedMultipartUpload{Parts: cp},
	})
	if err != nil {
		return nil, d.wrapErr("CompleteMultipartUpload", ref.Bucket, ref.Key, err)
	}
	// S3 的 complete 响应只带 ETag / VersionId / Checksum，**不含 Size 与
	// LastModified**。因此这里不伪造这两个字段：
	//   - Size 仅在调用方提供了全部 PartInfo.Size 时才等于各片之和，
	//     否则留 0 表示未知，由上层发一次 HeadObject 取得真值后再与
	//     init 时声明的大小对账。
	//   - 拿得到的是 S3 白送的 ETag，不必再 Head 一次（这正是原先只回 error
	//     而丢弃的字段）。
	info := &storage.ObjectInfo{
		Bucket:    ref.Bucket,
		Key:       ref.Key,
		ETag:      trimETag(aws.ToString(output.ETag)),
		VersionID: aws.ToString(output.VersionId),
		Checksum: checksumFrom(output.ChecksumCRC32, output.ChecksumCRC32C,
			output.ChecksumCRC64NVME, output.ChecksumSHA1, output.ChecksumSHA256),
	}
	if sizesKnown {
		info.Size = total
	}
	return info, nil
}

func (d *Driver) AbortMultipart(ctx context.Context, ref storage.MultipartRef) error {
	if err := pathcheck.ValidateBucket(ref.Bucket); err != nil {
		return err
	}
	if err := pathcheck.ValidateKey(ref.Key); err != nil {
		return err
	}
	_, err := d.client.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{
		Bucket:   aws.String(ref.Bucket),
		Key:      aws.String(ref.Key),
		UploadId: aws.String(ref.UploadID),
	})
	return d.wrapErr("AbortMultipartUpload", ref.Bucket, ref.Key, err)
}

// ---------- Ext ----------

func (d *Driver) HeadObject(ctx context.Context, bucket, key string) (*storage.ObjectInfo, error) {
	if err := pathcheck.ValidateBucket(bucket); err != nil {
		return nil, err
	}
	if err := pathcheck.ValidateKey(key); err != nil {
		return nil, err
	}
	output, err := d.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, d.wrapErr("HeadObject", bucket, key, err)
	}
	storageClass := string(output.StorageClass)
	info := &storage.ObjectInfo{
		Bucket:       bucket,
		Key:          key,
		Size:         aws.ToInt64(output.ContentLength),
		ETag:         trimETag(aws.ToString(output.ETag)),
		ContentType:  aws.ToString(output.ContentType),
		LastModified: aws.ToTime(output.LastModified).UTC(),
		StorageClass: storageClass,
		Archived:     isArchived(storageClass),
		VersionID:    aws.ToString(output.VersionId),
		Checksum: checksumFrom(output.ChecksumCRC32, output.ChecksumCRC32C,
			output.ChecksumCRC64NVME, output.ChecksumSHA1, output.ChecksumSHA256),
	}
	if output.Metadata != nil {
		info.Metadata = output.Metadata
	}
	return info, nil
}

func (d *Driver) CopyObject(ctx context.Context, srcBucket, srcKey, dstBucket, dstKey string) error {
	if err := pathcheck.ValidateBucket(srcBucket); err != nil {
		return err
	}
	if err := pathcheck.ValidateBucket(dstBucket); err != nil {
		return err
	}
	if err := pathcheck.ValidateKey(srcKey); err != nil {
		return err
	}
	if err := pathcheck.ValidateKey(dstKey); err != nil {
		return err
	}
	_, err := d.client.CopyObject(ctx, copyObjectInput(srcBucket, srcKey, dstBucket, dstKey))
	return d.wrapErr("CopyObject", dstBucket, dstKey, err)
}

// presign 结果统一组装：方法、URL、以及**必须原样发送的请求头**。
// SigV4 会把 Content-Type / Content-MD5 / X-Amz-Meta-* 纳入签名
// （出现在 X-Amz-SignedHeaders 中），只把 URL 交给前端会让这些头丢失，
// 服务端随即返回 SignatureDoesNotMatch。SignedHeader 正是为此存在。
func presignedRequest(method, url string, headers http.Header, ttl time.Duration) *storage.PresignedRequest {
	return &storage.PresignedRequest{
		Method:    method,
		URL:       url,
		Headers:   headers,
		ExpiresAt: time.Now().UTC().Add(ttl),
	}
}

func (d *Driver) PresignGetObject(ctx context.Context, bucket, key string, ttl time.Duration, opts ...storage.GetOption) (*storage.PresignedRequest, error) {
	if err := pathcheck.ValidateBucket(bucket); err != nil {
		return nil, err
	}
	if err := pathcheck.ValidateKey(key); err != nil {
		return nil, err
	}
	ttl, err := storage.ResolvePresignTTL(ttl)
	if err != nil {
		return nil, err
	}
	o := &storage.GetOptions{}
	for _, opt := range opts {
		opt(o)
	}
	input := &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	}
	if o.ByteRange != nil {
		input.Range = aws.String(fmt.Sprintf("bytes=%d-%d", o.ByteRange.Start, o.ByteRange.End))
	}
	req, err := d.presign.PresignGetObject(ctx, input, s3.WithPresignExpires(ttl))
	if err != nil {
		return nil, d.wrapErr("PresignGetObject", bucket, key, err)
	}
	return presignedRequest(req.Method, req.URL, req.SignedHeader, ttl), nil
}

func (d *Driver) PresignPutObject(ctx context.Context, bucket, key string, ttl time.Duration, opts ...storage.PutOption) (*storage.PresignedRequest, error) {
	if err := pathcheck.ValidateBucket(bucket); err != nil {
		return nil, err
	}
	if err := pathcheck.ValidateKey(key); err != nil {
		return nil, err
	}
	ttl, err := storage.ResolvePresignTTL(ttl)
	if err != nil {
		return nil, err
	}
	o := &storage.PutOptions{}
	for _, opt := range opts {
		opt(o)
	}
	input := &s3.PutObjectInput{
		Bucket:      aws.String(bucket),
		Key:         aws.String(key),
		ContentType: strPtr(o.ContentType),
		ContentMD5:  strPtr(o.ContentMD5),
		Metadata:    o.Metadata,
	}
	req, err := d.presign.PresignPutObject(ctx, input, s3.WithPresignExpires(ttl))
	if err != nil {
		return nil, d.wrapErr("PresignPutObject", bucket, key, err)
	}
	return presignedRequest(req.Method, req.URL, req.SignedHeader, ttl), nil
}

// PresignUploadPartObject 签发指向 UploadPart 的 SigV4 预签名请求，
// 客户端可直接把分片 PUT 到对象存储，无需经过业务服务。
func (d *Driver) PresignUploadPartObject(ctx context.Context, ref storage.MultipartRef, number int32, ttl time.Duration, _ ...storage.PutOption) (*storage.PresignedRequest, error) {
	if err := pathcheck.ValidateBucket(ref.Bucket); err != nil {
		return nil, err
	}
	if err := pathcheck.ValidateKey(ref.Key); err != nil {
		return nil, err
	}
	if ref.UploadID == "" || number <= 0 {
		return nil, fmt.Errorf("%w: upload_id and part_number are required", storage.ErrInvalidArgument)
	}
	ttl, err := storage.ResolvePresignTTL(ttl)
	if err != nil {
		return nil, err
	}
	input := &s3.UploadPartInput{
		Bucket:     aws.String(ref.Bucket),
		Key:        aws.String(ref.Key),
		UploadId:   aws.String(ref.UploadID),
		PartNumber: aws.Int32(number),
	}
	req, err := d.presign.PresignUploadPart(ctx, input, s3.WithPresignExpires(ttl))
	if err != nil {
		return nil, d.wrapErr("PresignUploadPart", ref.Bucket, ref.Key, err)
	}
	return presignedRequest(req.Method, req.URL, req.SignedHeader, ttl), nil
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func trimETag(etag string) string {
	return strings.Trim(etag, "\"")
}

// quoteETag 把规范化（无引号）的 ETag 还原成 S3 出站要求的带引号形态。
// UploadPart 返回与 CompleteMultipartUpload 回传必须同形态，
// 而契约对外的 ETag 统一无引号，因此只在出站处补回。
func quoteETag(etag string) string {
	etag = trimETag(etag)
	if etag == "" {
		return ""
	}
	return `"` + etag + `"`
}
