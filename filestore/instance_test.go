package filestore

import (
	"context"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func resetSingleton() {
	once = sync.Once{}
	defaultFS = nil
}

func TestSingleton_NotInitialized_Panics(t *testing.T) {
	resetSingleton()
	require.Panics(t, func() { Get() })
}

func TestSingleton_InitAndUse(t *testing.T) {
	resetSingleton()
	db := newTestDB(t)
	Init(db, &mockStorage{}, "test-bucket")

	fs := Get()
	require.NotNil(t, fs)

	detail, err := RecordUpload(context.Background(), RecordUploadRequest{
		ContentHash: "singleton-hash",
		Name:        "a.txt",
		Size:        10,
		StoragePath: "a.txt",
	})
	require.NoError(t, err)
	require.Equal(t, "singleton-hash", detail.ContentHash)

	found, hit, err := CheckExist(context.Background(), "singleton-hash")
	require.NoError(t, err)
	require.True(t, hit)
	require.Equal(t, "singleton-hash", found.ContentHash)
}

func TestSingleton_Init_Idempotent(t *testing.T) {
	resetSingleton()
	db := newTestDB(t)
	Init(db, &mockStorage{}, "test-bucket")
	fs1 := Get()

	Init(db, &mockStorage{}, "other-bucket")
	fs2 := Get()
	require.Same(t, fs1, fs2)
}

func TestSingleton_InitFailure_Panics(t *testing.T) {
	resetSingleton()

	dbfile := filepath.Join(t.TempDir(), "readonly.db")
	createDB, err := gorm.Open(sqlite.Open(dbfile), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, createDB.Exec("CREATE TABLE placeholder (id INTEGER PRIMARY KEY);").Error)
	sqlDB, err := createDB.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	roDB, err := gorm.Open(sqlite.Open("file:"+dbfile+"?mode=ro&cache=shared"), &gorm.Config{})
	require.NoError(t, err)

	require.Panics(t, func() {
		Init(roDB, &mockStorage{}, "test-bucket")
	})
}
