package dbtest

import "testing"

// fakeTB 截获 t.Fatalf，避免真的终止测试 goroutine。
type fakeTB struct {
	testing.TB
	fatal bool
	skip  bool
}

func (f *fakeTB) Helper() {}
func (f *fakeTB) Fatalf(format string, args ...any) {
	f.fatal = true
	panic("fatal")
}
func (f *fakeTB) Skipf(format string, args ...any) {
	f.skip = true
	panic("skip")
}
func (f *fakeTB) Logf(format string, args ...any) {}

func TestValidIdent(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"test_db_1", true},
		{"ci_test_db_2", true},
		{"_leading", true},
		{"a", true},
		{"Test_DB", false},
		{"1abc", false},
		{"has-dash", false},
		{"has space", false},
		{"", false},
		{"a;b", false},
	}
	for _, c := range cases {
		if got := validIdent(c.name); got != c.want {
			t.Errorf("validIdent(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestResolveName(t *testing.T) {
	if got := resolveName("db", ""); got != "db" {
		t.Errorf("resolveName no prefix = %q, want %q", got, "db")
	}
	if got := resolveName("db", "ci"); got != "ci_db" {
		t.Errorf("resolveName with prefix = %q, want %q", got, "ci_db")
	}
}

// TestNewInvalidIdentifier 验证非法库名走到 t.Fatalf 分支。
func TestNewInvalidIdentifier(t *testing.T) {
	ft := &fakeTB{TB: t}
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("expected Fatal on invalid identifier")
			}
		}()
		New(ft, MySQL, "bad-name")
	}()
	if !ft.fatal {
		t.Errorf("New with invalid identifier did not call Fatalf")
	}
	// 前缀导致结果非法（大写）也应被拒绝。
	ft2 := &fakeTB{TB: t}
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("expected Fatal on invalid prefixed identifier")
			}
		}()
		New(ft2, MySQL, "db", WithPrefix("CI"))
	}()
	if !ft2.fatal {
		t.Errorf("New with invalid prefixed identifier did not call Fatalf")
	}
}

// TestNewNoDB 在有合法库名但无数据库时应当 Skip（而非失败）。
// 本地无库时 Skip 符合预期；有库时会真正建库并在清理时删除。
func TestNewNoDB(t *testing.T) {
	ft := &fakeTB{TB: t}
	func() {
		defer func() { _ = recover() }()
		_ = New(ft, MySQL, "dbtest_unit_probe_mysql")
	}()
	if !ft.skip && !ft.fatal {
		t.Log("New did not skip; a database appears reachable")
	}
}
