package manager

import (
	"testing"
	"time"
)

// TestDetectDialect 覆盖 gorm dialector Name() 到 Dialect 的映射。
func TestDetectDialect(t *testing.T) {
	cases := []struct {
		name string
		want Dialect
	}{
		{"mysql", DialectMySQL},
		{"MySQL", DialectMySQL},
		{"postgres", DialectPostgres},
		{"postgresql", DialectPostgres},
		{"sqlite", DialectSQLite},
		{"sqlite3", DialectSQLite},
		{"unknown-db", DialectUnknown},
		{"", DialectUnknown},
	}
	for _, c := range cases {
		if got := DetectDialect(c.name); got != c.want {
			t.Errorf("DetectDialect(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestDialectSnapshots 用快照断言三方言生成的 SQL 片段。
func TestDialectSnapshots(t *testing.T) {
	type snap struct {
		now    string
		after  string
		lock   string
		empty  string
		insPri string
		insSuf string
		iso    string
	}
	want := map[Dialect]snap{
		DialectMySQL: {
			now:    "NOW(3)",
			after:  "DATE_ADD(NOW(3), INTERVAL ? MICROSECOND)",
			lock:   " FOR UPDATE SKIP LOCKED",
			empty:  "NULLIF(?, '')",
			insPri: "IGNORE ",
			insSuf: "",
			iso:    "SET TRANSACTION ISOLATION LEVEL READ COMMITTED",
		},
		DialectPostgres: {
			now:    "now()",
			after:  "now() + ? * interval '1 microsecond'",
			lock:   " FOR UPDATE SKIP LOCKED",
			empty:  "NULLIF(?, '')",
			insPri: "",
			insSuf: " ON CONFLICT DO NOTHING",
			iso:    "SET TRANSACTION ISOLATION LEVEL READ COMMITTED",
		},
		DialectSQLite: {
			now:    "strftime('%Y-%m-%d %H:%M:%f','now')",
			after:  "datetime('now', ?)",
			lock:   "",
			empty:  "NULLIF(?, '')",
			insPri: "",
			insSuf: " ON CONFLICT DO NOTHING",
			iso:    "",
		},
	}
	for d, w := range want {
		if got := d.nowExpr(); got != w.now {
			t.Errorf("%v nowExpr = %q, want %q", d, got, w.now)
		}
		if got := d.afterExpr(); got != w.after {
			t.Errorf("%v afterExpr = %q, want %q", d, got, w.after)
		}
		if got := d.claimLockClause(); got != w.lock {
			t.Errorf("%v claimLockClause = %q, want %q", d, got, w.lock)
		}
		if got := d.emptyKeyExpr(); got != w.empty {
			t.Errorf("%v emptyKeyExpr = %q, want %q", d, got, w.empty)
		}
		pri, suf := d.insertIgnoreClause()
		if pri != w.insPri || suf != w.insSuf {
			t.Errorf("%v insertIgnoreClause = (%q,%q), want (%q,%q)", d, pri, suf, w.insPri, w.insSuf)
		}
		if got := d.isolationReadCommittedSQL(); got != w.iso {
			t.Errorf("%v isolationRC = %q, want %q", d, got, w.iso)
		}
	}
}

// TestAfterArg 断言间隔参数按方言换算。
func TestAfterArg(t *testing.T) {
	d := 1500 * time.Millisecond
	if got := DialectMySQL.afterArg(d); got != int64(1_500_000) {
		t.Errorf("mysql afterArg = %v", got)
	}
	if got := DialectPostgres.afterArg(d); got != float64(1_500_000) {
		t.Errorf("pg afterArg = %v", got)
	}
	if got := DialectSQLite.afterArg(d); got != "+1.500 seconds" {
		t.Errorf("sqlite afterArg = %v", got)
	}
}

// TestDialectString 断言方言短名。
func TestDialectString(t *testing.T) {
	if DialectMySQL.String() != "mysql" || DialectPostgres.String() != "postgres" ||
		DialectSQLite.String() != "sqlite" || DialectUnknown.String() != "unknown" {
		t.Fatal("dialect String mismatch")
	}
}
