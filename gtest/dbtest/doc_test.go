package dbtest_test

import (
	"testing"

	"github.com/morehao/golib/gtest/dbtest"
)

// TestNewAPIShape 冻结 dbtest 的调用形态。
func TestNewAPIShape(t *testing.T) {
	_ = func(t testing.TB) {
		_ = dbtest.New(t, dbtest.MySQL, "test_db_1", dbtest.WithPrefix("ci"))
		_ = dbtest.New(t, dbtest.Postgres, "test_db_2", dbtest.WithDSN("..."), dbtest.WithReuse())
	}
}
