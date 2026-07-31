package dbgorm_test

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/morehao/golib/dbaccess/dbgorm"
	_ "github.com/morehao/golib/dbaccess/dbgorm/driver/sqlite"
	"github.com/morehao/golib/glog"
	_ "github.com/morehao/golib/glog/driver/slog"
	_ "github.com/morehao/golib/glog/driver/zap"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type calibModel struct {
	ID   uint
	Name string
}

func bizCallForCalib(db *gorm.DB) int {
	_, _, anchorLine, _ := runtime.Caller(0)
	db.Find(&[]calibModel{}) // anchorLine + 1
	return anchorLine + 1
}

func collectCallers(t *testing.T, dir string) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, time.Now().Format("20060102"), "*.log"))
	require.NoError(t, err)
	require.NotEmpty(t, files)
	re := regexp.MustCompile(`"caller":"([^"]+)"`)
	var out []string
	for _, f := range files {
		b, err := os.ReadFile(f)
		require.NoError(t, err)
		for _, m := range re.FindAllSubmatch(b, -1) {
			out = append(out, string(m[1]))
		}
	}
	return out
}

func TestGormCallerSkipDefaultConsistent(t *testing.T) {
	for _, lt := range []glog.LoggerType{glog.LoggerTypeZap, glog.LoggerTypeSlog} {
		dir := t.TempDir()
		cfg := &glog.LogConfig{
			Service:    "calib",
			Module:     "test",
			Level:      glog.DebugLevel,
			Writers:    []glog.WriterConfig{{Type: glog.WriterFile, Dir: dir}},
			LoggerType: lt,
		}
		db, err := dbgorm.New(&dbgorm.Config{URL: "sqlite://:memory:"}, dbgorm.WithLogConfig(cfg))
		require.NoError(t, err)
		require.NoError(t, db.AutoMigrate(&calibModel{}))

		bizLine := bizCallForCalib(db)

		sqlDB, err := db.DB()
		require.NoError(t, err)
		_ = sqlDB.Close()
		if lt == glog.LoggerTypeZap {
			time.Sleep(6 * time.Second)
		}

		callers := collectCallers(t, dir)
		found := false
		for _, c := range callers {
			idx := strings.LastIndex(c, ":")
			if idx < 0 {
				continue
			}
			line, _ := strconv.Atoi(c[idx+1:])
			if line == bizLine {
				found = true
				break
			}
		}
		assert.True(t, found, "%s: 默认 callerSkip 应定位到业务查询行 %d，实际 caller 列表: %v", lt, bizLine, callers)
	}
}
