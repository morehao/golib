package gtest_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/morehao/golib/gtest/gtest"
)

func TestParseEnv(t *testing.T) {
	src, err := gtest.ParseEnv([]byte("A=1\nB=hello world\n# comment\nC=\"quoted\"\n"))
	if err != nil {
		t.Fatalf("ParseEnv: %v", err)
	}
	if got := src.Get("A", ""); got != "1" {
		t.Errorf("A = %q, want 1", got)
	}
	if got := src.Get("B", ""); got != "hello world" {
		t.Errorf("B = %q, want %q", got, "hello world")
	}
	if got := src.Get("C", ""); got != "quoted" {
		t.Errorf("C = %q, want quoted", got)
	}

	if _, err := gtest.ParseEnv([]byte("BAD=\"unterminated\n")); err == nil {
		t.Errorf("expected parse error for malformed line")
	}
}

func TestLoadEnv(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("KEY=val\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	// 从深层目录向上查找。
	src, err := gtest.LoadEnv(nested)
	if err != nil {
		t.Fatalf("LoadEnv: %v", err)
	}
	if got := src.Get("KEY", ""); got != "val" {
		t.Errorf("KEY = %q, want val", got)
	}

	// 缺失：返回空 Source + nil error。
	empty := t.TempDir()
	src, err = gtest.LoadEnv(empty)
	if err != nil {
		t.Fatalf("LoadEnv missing: unexpected error %v", err)
	}
	if len(src) != 0 {
		t.Errorf("LoadEnv missing: want empty Source, got %v", src)
	}
}

func TestSourceGetImpl(t *testing.T) {
	src := gtest.Source{"K": "v", "EMPTY": ""}
	if got := src.Get("K", "def"); got != "v" {
		t.Errorf("Get(K) = %q, want v", got)
	}
	if got := src.Get("EMPTY", "def"); got != "def" {
		t.Errorf("Get(EMPTY) = %q, want def", got)
	}
	if got := src.Get("MISSING", "def"); got != "def" {
		t.Errorf("Get(MISSING) = %q, want def", got)
	}
}

func TestSourceMust(t *testing.T) {
	src := gtest.Source{"K": "v"}
	if got := src.Must(t, "K"); got != "v" {
		t.Errorf("Must(K) = %q, want v", got)
	}
	// 缺失键应 t.Fatalf；用子测试确认会失败（Goexit 会结束该子协程）。
	fake := &fatalTB{TB: t}
	runIsolated(func() { src.Must(fake, "ABSENT") })
	if !fake.failed {
		t.Errorf("expected Must to Fatalf on missing key")
	}
}

func TestSourceApply(t *testing.T) {
	const key = "GTEST_APPLY_TEST_KEY"

	t.Run("values_applied_and_existing_restored", func(t *testing.T) {
		os.Setenv(key, "existing")
		t.Cleanup(func() { os.Unsetenv(key) })
		t.Run("inner", func(t *testing.T) {
			gtest.Source{key: "newval"}.Apply(t)
			if got := os.Getenv(key); got != "newval" {
				t.Fatalf("after Apply: %q, want newval", got)
			}
		})
		// inner 已完成 => 其 Cleanup 已还原既有值。
		if got := os.Getenv(key); got != "existing" {
			t.Errorf("after inner cleanup: %q, want existing", got)
		}
	})

	t.Run("absent_key_is_unset_after_cleanup", func(t *testing.T) {
		os.Unsetenv(key)
		t.Run("inner", func(t *testing.T) {
			gtest.Source{key: "newval"}.Apply(t)
			if got := os.Getenv(key); got != "newval" {
				t.Fatalf("after Apply: %q, want newval", got)
			}
		})
		if _, ok := os.LookupEnv(key); ok {
			t.Errorf("after inner cleanup, key should be unset, got %q", os.Getenv(key))
		}
	})
}

func TestCopyTree(t *testing.T) {
	src := t.TempDir()
	if err := os.MkdirAll(filepath.Join(src, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "a.txt"), []byte("alpha"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "sub", "b.txt"), []byte("beta"), 0o644); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(t.TempDir(), "out")
	gtest.CopyTree(t, src, dst)

	got, err := os.ReadFile(filepath.Join(dst, "a.txt"))
	if err != nil || string(got) != "alpha" {
		t.Fatalf("a.txt = %q, err %v", got, err)
	}
	got, err = os.ReadFile(filepath.Join(dst, "sub", "b.txt"))
	if err != nil || string(got) != "beta" {
		t.Fatalf("sub/b.txt = %q, err %v", got, err)
	}
}

func TestGoldenCompare(t *testing.T) {
	// 在临时目录内构造 testdata/golden 并 chdir（Golden 使用相对路径）。
	dir := t.TempDir()
	goldenDir := filepath.Join(dir, "testdata", "golden")
	if err := os.MkdirAll(goldenDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(goldenDir, "sample.txt"), []byte("golden-content"), 0o644); err != nil {
		t.Fatal(err)
	}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(wd)

	// 匹配路径：不应报错。
	gtest.Golden(t, []byte("golden-content"), "sample.txt")

	// 不匹配：应 t.Errorf；用 fakeTB 捕获。
	fake := &fatalTB{TB: t}
	gtest.Golden(fake, []byte("different"), "sample.txt")
	if !fake.errored {
		t.Errorf("expected Golden to record error on mismatch")
	}
}

func TestGoldenWrite(t *testing.T) {
	dir := t.TempDir()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(wd)

	old := *gtest.Update
	*gtest.Update = true
	defer func() { *gtest.Update = old }()

	gtest.Golden(t, []byte("written"), "new.txt")

	got, err := os.ReadFile(filepath.Join(dir, "testdata", "golden", "new.txt"))
	if err != nil {
		t.Fatalf("golden write: %v", err)
	}
	if string(got) != "written" {
		t.Errorf("written golden = %q, want written", got)
	}
}

func TestRepoRoot(t *testing.T) {
	root := gtest.RepoRoot()
	if root == "" {
		t.Fatalf("RepoRoot returned empty; expected a module root")
	}
	// 当前仓库根下应有 go.mod。
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Errorf("RepoRoot %q has no go.mod: %v", root, err)
	}
}

func TestConfigPathAndRepoRootTempTree(t *testing.T) {
	// 构造临时仓库树并 chdir 进去，验证 RepoRoot/ConfigPath 的查树逻辑。
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfgDir := filepath.Join(root, "apps", "demo", "config")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}

	nested := filepath.Join(root, "pkg", "deep")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(nested); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(wd)

	wantRoot, _ := filepath.EvalSymlinks(root)
	if got := gtest.RepoRoot(); got != wantRoot {
		t.Errorf("RepoRoot = %q, want %q", got, wantRoot)
	}

	// 只有 config.yaml。
	if err := os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte("a: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wantCfg := filepath.Join(wantRoot, "apps", "demo", "config", "config.yaml")
	if got := gtest.ConfigPath("demo"); got != wantCfg {
		t.Errorf("ConfigPath = %q, want %q", got, wantCfg)
	}

	// 存在 config.test.yaml 时优先。
	if err := os.WriteFile(filepath.Join(cfgDir, "config.test.yaml"), []byte("a: 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wantTest := filepath.Join(wantRoot, "apps", "demo", "config", "config.test.yaml")
	if got := gtest.ConfigPath("demo"); got != wantTest {
		t.Errorf("ConfigPath = %q, want %q", got, wantTest)
	}

	// 不存在的 appDir -> 空串。
	if got := gtest.ConfigPath("nope"); got != "" {
		t.Errorf("ConfigPath(nope) = %q, want empty", got)
	}
}

func TestEventuallySuccess(t *testing.T) {
	calls := 0
	gtest.Eventually(context.Background(), t, func(ctx context.Context) error {
		calls++
		if calls < 3 {
			return errors.New("not ready")
		}
		return nil
	}, gtest.WithTimeout(time.Second), gtest.WithInterval(time.Millisecond))
	if calls != 3 {
		t.Errorf("calls = %d, want 3", calls)
	}
}

func TestEventuallyTimeout(t *testing.T) {
	fake := &fatalTB{TB: t}
	runIsolated(func() {
		gtest.Eventually(context.Background(), fake, func(ctx context.Context) error {
			return errors.New("still failing")
		}, gtest.WithTimeout(20*time.Millisecond), gtest.WithInterval(time.Millisecond))
	})
	if !fake.failed {
		t.Errorf("expected Eventually to Fatalf on timeout")
	}
}

func TestEventuallyContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	fake := &fatalTB{TB: t}
	runIsolated(func() {
		gtest.Eventually(ctx, fake, func(ctx context.Context) error {
			return errors.New("never")
		}, gtest.WithTimeout(time.Second), gtest.WithInterval(time.Millisecond))
	})
	if !fake.failed {
		t.Errorf("expected Eventually to Fatalf on canceled context")
	}
}

func TestStartImpl(t *testing.T) {
	booted := 0
	res := gtest.Start(t, "svc", func(tb testing.TB) (any, func()) {
		booted++
		return "handle", func() {}
	})
	if res != "handle" {
		t.Errorf("Start returned %v, want handle", res)
	}
	if booted != 1 {
		t.Errorf("boot called %d times, want 1", booted)
	}
}

// fatalTB 包装 testing.TB，捕获 Fatalf / Errorf 而不真正终止测试。
type fatalTB struct {
	testing.TB
	failed  bool
	errored bool
}

func (f *fatalTB) Helper() {}

func (f *fatalTB) Fatalf(format string, args ...any) {
	f.failed = true
	runtime.Goexit()
}

// runIsolated 在独立 goroutine 中运行 fn，吞掉 Fatalf 触发的 Goexit，
// 使测试能安全断言 fake 的 failed 标志。
func runIsolated(fn func()) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	<-done
}

func (f *fatalTB) Errorf(format string, args ...any) {
	f.errored = true
}

func (f *fatalTB) Cleanup(fn func()) {}
