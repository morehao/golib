package filestore

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// 默认建表由 TestNewAutoMigrate 覆盖；这里验证 WithoutAutoMigrate 选项与显式入口。
func TestWithoutAutoMigrate_SkipsAutoMigrate(t *testing.T) {
	db := newTestDB(t)

	fs, err := New(db, &mockStorage{}, "test-bucket", WithoutAutoMigrate())
	require.NoError(t, err)
	require.NotNil(t, fs)
	require.False(t, db.Migrator().HasTable(&FileEntity{}), "WithoutAutoMigrate 后不应建表")
	require.False(t, db.Migrator().HasTable(&FileUploadEntity{}))

	// 显式 Migrate 不受选项影响
	require.NoError(t, Migrate(db))
	require.True(t, db.Migrator().HasTable(&FileEntity{}))
	require.True(t, db.Migrator().HasTable(&FileUploadEntity{}))

	// nil 安全
	require.Error(t, Migrate(nil))
}
