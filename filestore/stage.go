package filestore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"strings"

	"github.com/google/uuid"
	"github.com/morehao/golib/storage"
)

// stagePrefix 暂存对象 key 前缀。暂存对象只在中转期存在，不进入文件记录表，
// 也不会出现在对外可见的业务路径里。
const stagePrefix = "stage/"

// StageObject 把 reader 流式写入暂存对象，同时计算内容 SHA256。
//
// 内存占用与数据体积无关（固定大小缓冲区），因此可直接用于大文件直传；
// opts 会透传给底层 PutObject（如 WithContentType，其值在提升为最终对象时一并继承）。
// 调用方读完请求后必须二选一收口：
//   - 成功：CommitStagedObject（内部会清理暂存对象）
//   - 放弃：DiscardObject
func (s *FileStore) StageObject(ctx context.Context, r io.Reader, opts ...storage.PutOption) (*StagedObject, error) {
	if r == nil {
		return nil, fmt.Errorf("%w: reader is required", ErrInvalidArgument)
	}

	stagePath := stagePrefix + uuid.NewString()
	hasher := sha256.New()
	res, err := s.st.PutObject(ctx, s.bucket, stagePath, io.TeeReader(r, hasher), opts...)
	if err != nil {
		return nil, fmt.Errorf("filestore.StageObject: put object: %w", err)
	}

	return &StagedObject{
		Path:   stagePath,
		Size:   res.Size, // 以实际写入字节数为准，不信任调用方声明的长度
		SHA256: hex.EncodeToString(hasher.Sum(nil)),
	}, nil
}

// DiscardObject 删除暂存对象，用于放弃上传（参数校验失败、客户端断流等）。
func (s *FileStore) DiscardObject(ctx context.Context, path string) error {
	if path == "" {
		return nil
	}
	if err := s.st.DeleteObject(ctx, s.bucket, path); err != nil {
		return fmt.Errorf("filestore.DiscardObject: %w", err)
	}
	return nil
}

// CommitStagedObject 把暂存对象提交为正式文件记录：
//
//  1. 命中相同 ContentHash 时按去重处理，直接丢弃暂存对象；
//  2. 否则把暂存对象提升为「内容寻址」的最终 key（服务端拷贝，本地为硬链接、
//     对象存储为服务端 Copy，均不经过应用内存）；
//  3. 落库为文件记录。
//
// 无论成功或失败，暂存对象都会被清理，调用方无需再调用 DiscardObject。
func (s *FileStore) CommitStagedObject(ctx context.Context, req CommitStagedObjectRequest) (detail *FileDetail, err error) {
	if req.StoragePath == "" || req.ContentHash == "" {
		return nil, fmt.Errorf("%w: content_hash and storage_path are required", ErrInvalidArgument)
	}
	// 暂存对象生命周期由本方法负责：从这一刻起，无论校验失败、提升失败还是成功，
	// 暂存对象都会被清理，避免校验不过的上传在存储里留下垃圾。
	defer func() {
		if delErr := s.DiscardObject(ctx, req.StoragePath); delErr != nil && err == nil {
			err = delErr
		}
	}()

	if err := validateDeclaredHash(req.ContentHash, req.SHA256); err != nil {
		return nil, err
	}

	// 去重命中：已有同内容对象，暂存对象直接丢弃
	fh, err := s.fileDao.GetByCond(ctx, &fileCond{ContentHash: req.ContentHash})
	if err == nil && fh != nil {
		return s.appendUploadRecord(ctx, fh, req)
	}

	finalPath := req.SHA256
	if err := s.st.CopyObject(ctx, s.bucket, req.StoragePath, s.bucket, finalPath); err != nil {
		return nil, fmt.Errorf("filestore.CommitStagedObject: promote staged object: %w", err)
	}

	fh, err = s.findOrCreateFile(ctx, req.ContentHash, req.Size, finalPath)
	if err != nil {
		return nil, fmt.Errorf("filestore.CommitStagedObject: %w", err)
	}
	return s.appendUploadRecord(ctx, fh, req)
}

// appendUploadRecord 写入一次上传行为记录。
func (s *FileStore) appendUploadRecord(ctx context.Context, fh *FileEntity, req CommitStagedObjectRequest) (*FileDetail, error) {
	upload := &FileUploadEntity{
		FileID:   fh.ID,
		Name:     req.Name,
		MimeType: req.MimeType,
		Scene:    req.Scene,
		Status:   FileStatusCompleted,
	}
	if err := s.uploadDao.Insert(ctx, upload); err != nil {
		return nil, fmt.Errorf("filestore.CommitStagedObject: create record: %w", err)
	}
	return s.fillFileDetail(ctx, upload)
}

// validateDeclaredHash 校验客户端声明的 ContentHash。
//
// ContentHash 只作为去重键参与索引，历史实现允许任意字符串（如测试用的 "custom-fp"），
// 因此这里只做「看起来像 SHA256」时的强校验：当声明值形如 64 位十六进制时，必须与
// 服务端流式计算出的 SHA256 一致，避免内容寻址的对象被写坏后仍被登记。
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
