package filestore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/morehao/golib/dbaccess/gormdao"
	"github.com/morehao/golib/gutil"
	"github.com/morehao/golib/storage"
	"gorm.io/gorm"
)

// objectKeyPrefix 服务端生成的对象 key 前缀。所有由服务端命名的对象都落在该前缀下，
// 便于按前缀做生命周期与清理策略。
const objectKeyPrefix = "files/"

type FileStore struct {
	fileDao        *gormdao.Dao[FileEntity, []FileEntity, string]
	uploadDao      *gormdao.Dao[FileUploadEntity, []FileUploadEntity, string]
	st             storage.Storage
	bucket         string
	signSecret     string
	maxUploadBytes int64
}

// New 创建文件存储组件。
//
// 默认**自动建表**（幂等）；需要自己掌控 DDL（共库统一流程，或运行时账号无 DDL 权限）
// 时传 WithoutAutoMigrate() 关掉隐式建表，并由外部流程先调一次 Migrate。
func New(db *gorm.DB, st storage.Storage, bucket string, opts ...StoreOption) (*FileStore, error) {
	if db == nil {
		return nil, fmt.Errorf("filestore.New: db is required: %w", ErrInvalidArgument)
	}
	o := defaultStoreOptions()
	for _, fn := range opts {
		fn(&o)
	}
	maxUploadBytes := o.maxUploadBytes
	if maxUploadBytes == 0 {
		maxUploadBytes = defaultMaxUploadBytes
	}

	if o.autoMigrate {
		if err := Migrate(db); err != nil {
			return nil, fmt.Errorf("filestore.New: auto-migrate: %w", err)
		}
	}

	getDB := func(ctx context.Context) *gorm.DB { return db.WithContext(ctx) }
	return &FileStore{
		fileDao:        gormdao.NewDao[FileEntity, []FileEntity, string](FileEntity{}.TableName(), "filestore.file", getDB, gormdao.WithoutSoftDelete()),
		uploadDao:      gormdao.NewDao[FileUploadEntity, []FileUploadEntity, string](FileUploadEntity{}.TableName(), "filestore.upload", getDB, gormdao.WithoutSoftDelete()),
		st:             st,
		bucket:         bucket,
		signSecret:     o.signSecret,
		maxUploadBytes: maxUploadBytes,
	}, nil
}

// MaxUploadBytes 返回单次上传允许的最大字节数，<=0 表示不限制。
// HTTP 层据此给请求体加上限，避免无边界地占用内存与磁盘。
func (s *FileStore) MaxUploadBytes() int64 {
	return s.maxUploadBytes
}

func (s *FileStore) GetExpiry() time.Duration {
	return defaultPresignExpiry
}

func (s *FileStore) SignSecret() string {
	return s.signSecret
}

func (s *FileStore) IsLocal() bool {
	return s.st.PathBuilder().Build(s.bucket, "").IsLocal()
}

// PathBuilder 暴露底层驱动的路径渲染器，供 HTTP 层把 bucket/key 渲染为对外 URI。
// 对象元数据（ObjectInfo）不再携带路径，URI 渲染统一由 PathBuilder 负责。
func (s *FileStore) PathBuilder() storage.PathBuilder {
	return s.st.PathBuilder()
}

func (s *FileStore) buildStorageURI(storagePath string) string {
	return s.st.PathBuilder().Build(s.bucket, storagePath).URI()
}

func (s *FileStore) parseStorageURI(uri string) (scheme, bucket, key string, err error) {
	if uri == "" {
		return "", s.bucket, "", fmt.Errorf("%w: storage_uri is empty", ErrFileNotFound)
	}
	scheme, bucket, key, err = storage.ParseURI(uri)
	if err != nil {
		return "", "", "", err
	}
	if bucket == "" {
		bucket = s.bucket
	}
	return scheme, bucket, key, nil
}

// newObjectKey 生成服务端对象 key。客户端无法影响其取值，
// 因此不存在"指定 key 覆盖他人对象"的越权面。
// 按 ID 前两位分目录，避免单一前缀下对象过度集中。
func (s *FileStore) newObjectKey() (string, error) {
	id := gutil.GenUUID()
	if len(id) < 2 {
		return "", fmt.Errorf("filestore.newObjectKey: generated id too short: %q", id)
	}
	return objectKeyPrefix + id[:2] + "/" + id, nil
}

func (s *FileStore) fillFileDetail(ctx context.Context, upload *FileUploadEntity) (*FileDetail, error) {
	detail := &FileDetail{
		FileUploadID: upload.ID,
		CreatedAt:    upload.CreatedAt,
		UpdatedAt:    upload.UpdatedAt,
		FileID:       upload.FileID,
		UploadID:     upload.UploadID,
		Name:         upload.Name,
		MimeType:     upload.MimeType,
		Status:       upload.Status,
		Scene:        upload.Scene,
	}
	if upload.FileID == "" {
		return detail, nil
	}
	fh, err := s.fileDao.GetByID(ctx, upload.FileID)
	if err != nil {
		return nil, err
	}
	if fh == nil {
		return nil, fmt.Errorf("%w: file_id=%s", ErrFileNotFound, upload.FileID)
	}
	detail.ContentHash = fh.ContentHash
	detail.Size = fh.Size
	detail.StorageURI = fh.StorageURI
	return detail, nil
}

// findOrCreateFile 按 content_hash 查找物理文件记录；不存在则用 key 落库新建。
// created=true 表示本次调用新建了记录（调用方据此判断"本次上传真正产生了新对象"）；
// created=false 表示命中了已有内容，key 未被使用，调用方不得写任何字节。
//
// 保留插入竞态处理：并发插入同 content_hash 时唯一索引会让其中一个失败，
// 失败方按 content_hash 回查，查到即视为命中（created=false），而不是向上抛错。
func (s *FileStore) findOrCreateFile(ctx context.Context, contentHash string, size int64, key string) (*FileEntity, bool, error) {
	fh, err := s.fileDao.GetByCond(ctx, &fileCond{ContentHash: contentHash})
	if err != nil {
		return nil, false, fmt.Errorf("findOrCreateFile: get hash: %w", err)
	}
	if fh != nil {
		return fh, false, nil
	}

	fh = &FileEntity{
		ContentHash: contentHash,
		Size:        size,
		StorageURI:  s.buildStorageURI(key),
	}
	if err := s.fileDao.Insert(ctx, fh); err != nil {
		found, lookupErr := s.fileDao.GetByCond(ctx, &fileCond{ContentHash: contentHash})
		if lookupErr == nil && found != nil {
			return found, false, nil
		}
		return nil, false, fmt.Errorf("findOrCreateFile: create hash: %w", err)
	}
	return fh, true, nil
}

func (s *FileStore) Open(ctx context.Context, id string) (io.ReadCloser, *FileDetail, error) {
	detail, err := s.GetFile(ctx, id)
	if err != nil {
		return nil, nil, fmt.Errorf("filestore.Open: %w", err)
	}

	_, bucket, key, err := s.parseStorageURI(detail.StorageURI)
	if err != nil {
		return nil, nil, fmt.Errorf("filestore.Open: %w", err)
	}

	result, err := s.st.GetObject(ctx, bucket, key)
	if err != nil {
		return nil, nil, fmt.Errorf("filestore.Open: get object: %w", err)
	}

	return result.Body, detail, nil
}

func (s *FileStore) CheckExist(ctx context.Context, contentHash string) (*FileDetail, bool, error) {
	fh, err := s.fileDao.GetByCond(ctx, &fileCond{ContentHash: contentHash})
	if err != nil {
		return nil, false, fmt.Errorf("filestore.CheckExist: %w", err)
	}
	if fh == nil {
		return nil, false, nil
	}

	return &FileDetail{
		FileID:      fh.ID,
		ContentHash: fh.ContentHash,
		Size:        fh.Size,
		StorageURI:  fh.StorageURI,
	}, true, nil
}

func (s *FileStore) UploadAndRecord(ctx context.Context, req UploadAndRecordRequest) (*FileDetail, error) {
	if req.ContentHash == "" || req.Reader == nil {
		return nil, fmt.Errorf("%w: content_hash and reader are required", ErrInvalidArgument)
	}

	fh, err := s.fileDao.GetByCond(ctx, &fileCond{ContentHash: req.ContentHash})
	hit := err == nil && fh != nil

	if !hit {
		// 对象 key 完全由服务端生成，客户端无法指定落点。
		key, keyErr := s.newObjectKey()
		if keyErr != nil {
			return nil, fmt.Errorf("filestore.UploadAndRecord: %w", keyErr)
		}

		// 边写边计数与哈希：S3 的 PutObject 响应不返回权威 Size（仅 S3 Express 追加
		// 场景才有值），因此落库大小以驱动实际消费的字节数为准；driver 未消费 reader
		// （如测试 mock）时退回调用方声明值。
		hasher := sha256.New()
		counter := &countingReader{r: io.TeeReader(req.Reader, hasher)}
		var putOpts []storage.PutOption
		if req.MimeType != "" {
			putOpts = append(putOpts, storage.WithContentType(req.MimeType))
		}
		if _, err := s.st.PutObject(ctx, s.bucket, key, counter, putOpts...); err != nil {
			return nil, fmt.Errorf("filestore.UploadAndRecord: put object: %w", err)
		}
		size := counter.n
		if size == 0 {
			size = req.Size
		}

		// 声明的 content_hash 形如 SHA256 时必须与实测一致：否则客户端可以拿假哈希
		// 污染去重表（把自己的内容登记成他人哈希，后续同哈希请求会拿到错误内容）。
		// reader 未被驱动消费时无法实测，跳过校验（此时 counter.n == 0）。
		if counter.n > 0 {
			actual := hex.EncodeToString(hasher.Sum(nil))
			if hashErr := validateDeclaredHash(req.ContentHash, actual); hashErr != nil {
				_ = s.st.DeleteObject(ctx, s.bucket, key)
				return nil, fmt.Errorf("filestore.UploadAndRecord: %w", hashErr)
			}
		}

		fh = &FileEntity{
			ContentHash: req.ContentHash,
			Size:        size,
			StorageURI:  s.buildStorageURI(key),
		}
		createErr := s.fileDao.Insert(ctx, fh)
		if createErr != nil {
			// 插入竞态：本次对象没有任何 DB 行引用，先删掉再按 content_hash 回查。
			_ = s.st.DeleteObject(ctx, s.bucket, key)
			found, lookupErr := s.fileDao.GetByCond(ctx, &fileCond{ContentHash: req.ContentHash})
			if lookupErr == nil && found != nil {
				fh = found
			} else {
				return nil, fmt.Errorf("filestore.UploadAndRecord: create file hash: %w", createErr)
			}
		}
	}

	upload := &FileUploadEntity{
		FileID:   fh.ID,
		Name:     req.Name,
		MimeType: req.MimeType,
		Scene:    req.Scene,
		Status:   FileStatusCompleted,
	}
	if err := s.uploadDao.Insert(ctx, upload); err != nil {
		return nil, fmt.Errorf("filestore.UploadAndRecord: create record: %w", err)
	}

	return s.fillFileDetail(ctx, upload)
}

func (s *FileStore) GetFile(ctx context.Context, id string) (*FileDetail, error) {
	upload, err := s.uploadDao.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("filestore.GetFile: %w", err)
	}
	if upload == nil {
		return nil, fmt.Errorf("%w: id=%s", ErrFileNotFound, id)
	}
	return s.fillFileDetail(ctx, upload)
}

func (s *FileStore) PresignGetFileURL(ctx context.Context, id string, opts ...PresignOption) (*storage.PresignedRequest, error) {
	detail, err := s.GetFile(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("filestore.PresignGetFileURL: %w", err)
	}

	_, bucket, key, err := s.parseStorageURI(detail.StorageURI)
	if err != nil {
		return nil, fmt.Errorf("filestore.PresignGetFileURL: %w", err)
	}

	expires := applyPresignOptions(opts...)
	presigned, err := s.st.PresignGetObject(ctx, bucket, key, expires)
	if err != nil {
		return nil, fmt.Errorf("filestore.PresignGetFileURL: %w", err)
	}
	return presigned, nil
}

// DeleteFile 删除一条上传记录，并在该物理文件不再被任何记录引用时回收存储对象，
// 避免留下「数据库无记录、存储有对象」的孤儿（此前只删记录，对象永久泄漏）。
//
// 顺序说明：先删记录再删对象。若对象删除失败，错误会返回给调用方；此时对象暂时成为
// 孤儿（无记录引用），但内容寻址的 key 会在下一次同内容上传时被重建/覆盖，运维也可按
// core_file 表与存储对象比对清理。反过来先删对象，一旦记录删除失败就会留下
// 「记录指向不存在的对象」的坏数据。
func (s *FileStore) DeleteFile(ctx context.Context, id string) error {
	rec, err := s.uploadDao.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("filestore.DeleteFile: %w", err)
	}
	if rec == nil {
		return fmt.Errorf("%w: upload_id=%s", ErrFileNotFound, id)
	}

	if err := s.uploadDao.Delete(ctx, id, ""); err != nil {
		return fmt.Errorf("filestore.DeleteFile: %w", err)
	}

	remaining, err := s.uploadDao.CountByCond(ctx, &fileUploadCond{FileID: rec.FileID})
	if err != nil {
		return fmt.Errorf("filestore.DeleteFile: count refs: %w", err)
	}
	if remaining > 0 {
		return nil
	}

	fh, err := s.fileDao.GetByID(ctx, rec.FileID)
	if err != nil {
		return fmt.Errorf("filestore.DeleteFile: %w", err)
	}
	if fh == nil {
		return nil
	}

	if _, bucket, key, parseErr := s.parseStorageURI(fh.StorageURI); parseErr == nil {
		if delErr := s.st.DeleteObject(ctx, bucket, key); delErr != nil && !errors.Is(delErr, storage.ErrNotFound) {
			return fmt.Errorf("filestore.DeleteFile: delete object %s: %w", fh.StorageURI, delErr)
		}
	}
	if err := s.fileDao.Delete(ctx, fh.ID, ""); err != nil {
		return fmt.Errorf("filestore.DeleteFile: %w", err)
	}
	return nil
}

// countingReader 统计底层 reader 被实际消费的字节数。
// 用于在服务端代理上传路径上得到权威的对象大小（不采信 driver 的 PutObject 响应）。
type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

// validateDeclaredHash 校验客户端声明的 ContentHash。
//
// ContentHash 只作为去重键参与索引，历史实现允许任意字符串（如测试用的 "custom-fp"），
// 因此这里只做「看起来像 SHA256」时的强校验：当声明值形如 64 位十六进制时，必须与
// 服务端流式计算出的 SHA256 一致，避免内容被写坏后仍以他人哈希登记。
func validateDeclaredHash(declared, actual string) error {
	if len(declared) != 64 || !isHex(declared) {
		return nil
	}
	if !strings.EqualFold(declared, actual) {
		return fmt.Errorf("%w: content_hash does not match uploaded content", ErrHashMismatch)
	}
	return nil
}

func isHex(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'f', c >= 'A' && c <= 'F':
		default:
			return false
		}
	}
	return true
}

func (s *FileStore) GetFileUploadIDByStorageURI(ctx context.Context, storageURI string) (string, error) {
	fh, err := s.fileDao.GetByCond(ctx, &fileCond{StorageURI: storageURI})
	if err != nil {
		return "", fmt.Errorf("filestore.GetFileUploadIDByStorageURI: %w", err)
	}
	if fh == nil {
		return "", fmt.Errorf("%w: storage_uri=%s", ErrFileNotFound, storageURI)
	}

	rec, err := s.uploadDao.GetByCond(ctx, &fileUploadCond{
		FileID: fh.ID,
		Status: FileStatusCompleted,
	})
	if err != nil {
		return "", fmt.Errorf("filestore.GetFileUploadIDByStorageURI: %w", err)
	}
	if rec == nil {
		return "", fmt.Errorf("%w: file_id=%s from storage_uri=%s", ErrFileNotFound, fh.ID, storageURI)
	}
	return rec.ID, nil
}

// InitMultipartUpload 创建分片上传会话。顺序是本次改造的核心：
//
//  1. 校验参数（content_hash 非空、size > 0）；
//  2. 服务端生成 key（纯函数，零副作用）；
//  3. 按 content_hash 落库去重 —— 命中已有内容直接返回 ErrContentExists，
//     **不创建分片会话、不写任何字节**（旧实现先 CreateMultipart 再查库，
//     去重命中时客户端已上传的字节会落在客户端指定的 key 下且无人引用）；
//  4. 创建分片会话，失败则删除第 3 步刚插入的 FileEntity（它指向不存在的对象）；
//  5. 写入上传记录，失败则 AbortMultipart 并同样回滚 FileEntity。
func (s *FileStore) InitMultipartUpload(ctx context.Context, req InitMultipartUploadRequest) (*FileDetail, error) {
	if req.ContentHash == "" {
		return nil, fmt.Errorf("%w: content_hash is required", ErrInvalidArgument)
	}
	if req.Size <= 0 {
		return nil, fmt.Errorf("%w: size must be positive", ErrInvalidArgument)
	}

	key, err := s.newObjectKey()
	if err != nil {
		return nil, fmt.Errorf("filestore.InitMultipartUpload: %w", err)
	}

	fh, created, err := s.findOrCreateFile(ctx, req.ContentHash, req.Size, key)
	if err != nil {
		return nil, fmt.Errorf("filestore.InitMultipartUpload: %w", err)
	}
	if !created {
		// 去重命中：本次不产生任何副作用，调用方应直接复用已有文件。
		return nil, fmt.Errorf("%w: content_hash=%s file_id=%s", ErrContentExists, req.ContentHash, fh.ID)
	}

	uploadID, err := s.st.CreateMultipart(ctx, s.bucket, key, storage.CreateMultipartInput{
		ContentType: req.MimeType,
		Size:        req.Size,
	})
	if err != nil {
		// 回滚：刚插入的 FileEntity 指向一个不存在的对象，留着会让
		// /files/check-exist 报"存在"但下载 404。
		if delErr := s.fileDao.Delete(ctx, fh.ID, ""); delErr != nil {
			return nil, fmt.Errorf("filestore.InitMultipartUpload: create multipart upload: %w (rollback file record id=%s: %v)", err, fh.ID, delErr)
		}
		return nil, fmt.Errorf("filestore.InitMultipartUpload: create multipart upload: %w", err)
	}

	upload := &FileUploadEntity{
		FileID:   fh.ID,
		Name:     req.Name,
		MimeType: req.MimeType,
		Scene:    req.Scene,
		UploadID: uploadID,
		Status:   FileStatusUploading,
	}
	if err := s.uploadDao.Insert(ctx, upload); err != nil {
		_ = s.st.AbortMultipart(ctx, storage.MultipartRef{Bucket: s.bucket, Key: key, UploadID: uploadID})
		_ = s.fileDao.Delete(ctx, fh.ID, "")
		return nil, fmt.Errorf("filestore.InitMultipartUpload: create record: %w", err)
	}

	return s.fillFileDetail(ctx, upload)
}

func (s *FileStore) PresignUploadPartURL(ctx context.Context, id string, partNum int32, opts ...PresignOption) (*storage.PresignedRequest, error) {
	detail, err := s.GetFile(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("filestore.PresignUploadPartURL: %w", err)
	}
	if detail.UploadID == "" {
		return nil, fmt.Errorf("%w: id=%s", ErrNotMultipartUpload, id)
	}
	if partNum <= 0 {
		return nil, fmt.Errorf("%w: part_number must be positive", ErrInvalidArgument)
	}

	_, bucket, key, err := s.parseStorageURI(detail.StorageURI)
	if err != nil {
		return nil, fmt.Errorf("filestore.PresignUploadPartURL: %w", err)
	}

	expires := applyPresignOptions(opts...)
	// 分片 URL 必须落在该次分片会话上：把 upload_id 与 part_number 交给 driver
	// 一起签发（S3 后端签 UploadPart，local 后端把二者写进 token）。
	// 返回的 PresignedRequest 含签名覆盖的 Headers，HTTP 层必须原样透传给前端。
	presigned, err := s.st.PresignUploadPartObject(ctx, storage.MultipartRef{
		Bucket:   bucket,
		Key:      key,
		UploadID: detail.UploadID,
	}, partNum, expires)
	if err != nil {
		return nil, fmt.Errorf("filestore.PresignUploadPartURL: presign: %w", err)
	}
	return presigned, nil
}

func (s *FileStore) CompleteMultipartUpload(ctx context.Context, req CompleteMultipartUploadRequest) (*FileDetail, error) {
	detail, err := s.GetFile(ctx, req.ID)
	if err != nil {
		return nil, fmt.Errorf("filestore.CompleteMultipartUpload: %w", err)
	}
	if detail.UploadID == "" {
		return nil, fmt.Errorf("%w: id=%s", ErrNotMultipartUpload, req.ID)
	}

	_, bucket, key, err := s.parseStorageURI(detail.StorageURI)
	if err != nil {
		return nil, fmt.Errorf("filestore.CompleteMultipartUpload: %w", err)
	}

	if err := s.uploadDao.UpdateMap(ctx, req.ID, map[string]any{"status": FileStatusMerging}); err != nil {
		return nil, fmt.Errorf("filestore.CompleteMultipartUpload: update status to merging: %w", err)
	}

	info, err := s.st.CompleteMultipart(ctx, storage.MultipartRef{
		Bucket:   bucket,
		Key:      key,
		UploadID: detail.UploadID,
	}, req.Parts)
	if err != nil {
		_ = s.uploadDao.UpdateMap(ctx, req.ID, map[string]any{"status": FileStatusUploading})
		return nil, fmt.Errorf("filestore.CompleteMultipartUpload: complete: %w", err)
	}

	// 与 init 时声明的 Size 对账。
	//
	// 校验边界（必须如实说明）：这能抓住"init 声明 100MB、实际分片合计只有 10MB"这类
	// 客户端与服务端不一致的提交；**抓不住**"客户端在 init 与 complete 处一致地撒谎"，
	// 因为 S3 的 complete 响应里没有权威 Size（协议事实），要堵住只能额外发一次
	// HeadObject，本期明确不做。
	if info != nil && info.Size > 0 {
		fh, fhErr := s.fileDao.GetByID(ctx, detail.FileID)
		if fhErr != nil {
			return nil, fmt.Errorf("filestore.CompleteMultipartUpload: get file record: %w", fhErr)
		}
		if fh != nil && fh.Size > 0 && info.Size != fh.Size {
			// 大小不符：对象已合并但内容与声明不一致。删除对象与本次刚插入的
			// FileEntity，并把上传记录的文件引用清空（会话已结束，无法 abort），
			// 避免留下"DB 记录指向错误内容"的孤儿。
			_ = s.st.DeleteObject(ctx, bucket, key)
			_ = s.fileDao.Delete(ctx, fh.ID, "")
			_ = s.uploadDao.UpdateMap(ctx, req.ID, map[string]any{
				"file_id":   "",
				"upload_id": "",
				"status":    FileStatusAborted,
			})
			return nil, fmt.Errorf("%w: declared=%d actual=%d", ErrSizeMismatch, fh.Size, info.Size)
		}
	}

	if err := s.uploadDao.UpdateMap(ctx, req.ID, map[string]any{
		"upload_id": "",
		"status":    FileStatusCompleted,
	}); err != nil {
		return nil, fmt.Errorf("filestore.CompleteMultipartUpload: clear upload id: %w", err)
	}

	updated, err := s.GetFile(ctx, req.ID)
	if err != nil {
		return nil, fmt.Errorf("filestore.CompleteMultipartUpload: get updated: %w", err)
	}
	return updated, nil
}

// ListParts 列出某个分片上传会话中已成功上传的分片，供客户端崩溃后续传。
// 受底层驱动的 Caps().ListParts 约束：不支持时返回 storage.ErrNotSupported。
func (s *FileStore) ListParts(ctx context.Context, id string, opts ...storage.ListPartsOption) (*storage.ListPartsOutput, error) {
	detail, err := s.GetFile(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("filestore.ListParts: %w", err)
	}
	if detail.UploadID == "" {
		return nil, fmt.Errorf("%w: id=%s", ErrNotMultipartUpload, id)
	}
	if !s.st.Caps().ListParts {
		return nil, fmt.Errorf("filestore.ListParts: %w", storage.ErrNotSupported)
	}

	_, bucket, key, err := s.parseStorageURI(detail.StorageURI)
	if err != nil {
		return nil, fmt.Errorf("filestore.ListParts: %w", err)
	}

	out, err := s.st.ListParts(ctx, storage.MultipartRef{
		Bucket:   bucket,
		Key:      key,
		UploadID: detail.UploadID,
	}, opts...)
	if err != nil {
		return nil, fmt.Errorf("filestore.ListParts: %w", err)
	}
	if out == nil { // 驱动返回 (nil, nil) 时按空列表处理，避免上层解引用 panic
		out = &storage.ListPartsOutput{}
	}
	return out, nil
}

func (s *FileStore) AbortMultipartUpload(ctx context.Context, id string) error {
	detail, err := s.GetFile(ctx, id)
	if err != nil {
		return fmt.Errorf("filestore.AbortMultipartUpload: %w", err)
	}
	if detail.UploadID == "" {
		return fmt.Errorf("%w: id=%s", ErrNotMultipartUpload, id)
	}

	_, bucket, key, err := s.parseStorageURI(detail.StorageURI)
	if err != nil {
		return fmt.Errorf("filestore.AbortMultipartUpload: %w", err)
	}

	if err := s.st.AbortMultipart(ctx, storage.MultipartRef{
		Bucket:   bucket,
		Key:      key,
		UploadID: detail.UploadID,
	}); err != nil {
		return fmt.Errorf("filestore.AbortMultipartUpload: abort: %w", err)
	}

	if err := s.uploadDao.UpdateMap(ctx, id, map[string]any{"status": FileStatusAborted}); err != nil {
		return fmt.Errorf("filestore.AbortMultipartUpload: update status: %w", err)
	}
	return nil
}
