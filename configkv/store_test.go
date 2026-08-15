package configkv

import (
	"context"
	"fmt"
	"testing"

	"github.com/morehao/golib/internal/testutil"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestStoreSet_Upsert(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&ConfigEntity{}))

	s := newStore(func(ctx context.Context) *gorm.DB { return db.WithContext(ctx) }, nil, nil)
	ctx := context.Background()

	// 首次 Set：插入，BeforeCreate 生成 UUID 主键
	e1 := &ConfigEntity{GroupName: "g", Key: "k", ValueType: ValueTypeString, Value: "v1", Status: StatusEnabled}
	require.NoError(t, s.Set(ctx, e1))
	require.NotEmpty(t, e1.ID)

	// 同 (group,key) 重复 Set：应覆盖更新，而非撞 uk_group_key 唯一索引
	e2 := &ConfigEntity{GroupName: "g", Key: "k", ValueType: ValueTypeString, Value: "v2", Status: StatusDisabled}
	require.NoError(t, s.Set(ctx, e2))

	// 仅一行，值已更新，主键保持首次插入的 ID
	var count int64
	require.NoError(t, db.Model(&ConfigEntity{}).Count(&count).Error)
	require.Equal(t, int64(1), count)

	got, err := s.Get(ctx, "g", "k")
	require.NoError(t, err)
	require.Equal(t, "v2", got.Value)
	require.Equal(t, StatusDisabled, got.Status)
	require.Equal(t, e1.ID, got.ID)

	// 不同 key 互不影响，仍为新插入
	e3 := &ConfigEntity{GroupName: "g", Key: "k2", ValueType: ValueTypeString, Value: "x"}
	require.NoError(t, s.Set(ctx, e3))
	require.NoError(t, db.Model(&ConfigEntity{}).Count(&count).Error)
	require.Equal(t, int64(2), count)

	// SetEncrypted 复用 Set，同样幂等（重复设置同 (group,key) 不报唯一索引冲突）
	crypto, err := newAESCrypto()
	require.NoError(t, err)
	s.crypto = crypto
	require.NoError(t, s.SetEncrypted(ctx, "g", "k", ValueTypeString, "v3"))
	require.NoError(t, s.SetEncrypted(ctx, "g", "k", ValueTypeString, "v4"))

	// 读到的为解密后的明文，落库为密文
	got2, err := s.Get(ctx, "g", "k")
	require.NoError(t, err)
	require.Equal(t, "v4", got2.Value)
	require.Equal(t, EncryptionModeEncrypted, got2.EncryptionMode)

	var raw ConfigEntity
	require.NoError(t, db.Where("group_name = ? AND key = ?", "g", "k").First(&raw).Error)
	require.Equal(t, EncryptionModeEncrypted, raw.EncryptionMode)
	require.Contains(t, raw.Value, "enc:")

	require.NoError(t, db.Model(&ConfigEntity{}).Count(&count).Error)
	require.Equal(t, int64(2), count)
}

// TestStoreSet_UpsertPostgres 回归场景：store.Set 原用 Save + 空 ID 实体，
// 重复 Set 在 PG 上报 "duplicate key value violates unique constraint"，
// 现改为按 (group_name, key) ON CONFLICT upsert。事务内执行并回滚。
func TestStoreSet_UpsertPostgres(t *testing.T) {
	dsn := testutil.GetEnv(testutil.PostgresDSN, "host=127.0.0.1 user=postgres password=123456 dbname=demo port=5432 sslmode=disable TimeZone=Asia/Shanghai")
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Skipf("skip postgres-dependent test: %v", err)
	}

	db.Transaction(func(tx *gorm.DB) error {
		require.NoError(t, tx.Migrator().DropTable(&ConfigEntity{}))
		require.NoError(t, tx.AutoMigrate(&ConfigEntity{}))

		s := newStore(func(ctx context.Context) *gorm.DB { return tx.WithContext(ctx) }, nil, nil)
		ctx := context.Background()

		e1 := &ConfigEntity{GroupName: "g", Key: "k", ValueType: ValueTypeString, Value: "v1"}
		require.NoError(t, s.Set(ctx, e1))

		e2 := &ConfigEntity{GroupName: "g", Key: "k", ValueType: ValueTypeString, Value: "v2", Status: StatusDisabled}
		require.NoError(t, s.Set(ctx, e2))

		var count int64
		require.NoError(t, tx.Model(&ConfigEntity{}).Count(&count).Error)
		require.Equal(t, int64(1), count)

		got, err := s.Get(ctx, "g", "k")
		require.NoError(t, err)
		require.Equal(t, "v2", got.Value)
		require.Equal(t, StatusDisabled, got.Status)
		require.Equal(t, e1.ID, got.ID)

		return fmt.Errorf("rollback:keep-clean")
	})
}
